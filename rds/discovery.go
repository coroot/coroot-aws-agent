package rds

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/arn"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/rds"
	"github.com/aws/aws-sdk-go/service/rds/rdsiface"
	"github.com/coroot/logger"
	"github.com/prometheus/client_golang/prometheus"
	"strings"
	"time"
)

type Discoverer struct {
	reg prometheus.Registerer

	awsSession *session.Session

	discoveryInterval time.Duration

	dbConf DbCollectorConf

	instances map[string]*Collector

	logger logger.Logger
}

func NewDiscoverer(reg prometheus.Registerer, awsSession *session.Session, discoveryInterval time.Duration, dbConf DbCollectorConf) *Discoverer {
	d := &Discoverer{
		reg:               reg,
		awsSession:        awsSession,
		discoveryInterval: discoveryInterval,
		dbConf:            dbConf,
		instances:         map[string]*Collector{},
		logger:            logger.NewKlog(""),
	}
	return d
}

func (d *Discoverer) Run() {
	api := rds.New(d.awsSession)

	if err := d.refresh(api); err != nil {
		d.logger.Warning(err)
	}

	ticker := time.Tick(d.discoveryInterval)
	for range ticker {
		if err := d.refresh(api); err != nil {
			d.logger.Warning(err)
		}
	}
}

func (d *Discoverer) refresh(api rdsiface.RDSAPI) error {
	t := time.Now()
	defer func() {
		d.logger.Info("instances refreshed in:", time.Since(t))
	}()

	output, err := api.DescribeDBInstances(nil)
	if err != nil {
		return err
	}

	actualInstances := map[string]bool{}
	for _, dbInstance := range output.DBInstances {
		if dbInstance.Endpoint == nil {
			continue
		}
		id := aws.StringValue(dbInstance.DBInstanceIdentifier)
		actualInstances[id] = true
		i, ok := d.instances[id]
		if !ok {
			d.logger.Info("new DB instance found:", id)
			i, err = NewCollector(d.awsSession, dbInstance, d.dbConf)
			if err != nil {
				d.logger.Warning("failed to init RDS collector:", err)
				continue
			}
			if err := d.wrappedReg(id).Register(i); err != nil {
				d.logger.Warning(err)
				continue
			}
			d.instances[id] = i
		}
		i.update(dbInstance)
	}

	for id, i := range d.instances {
		if !actualInstances[id] {
			d.logger.Info("instance no longer exists:", id)
			d.wrappedReg(id).Unregister(i)
			i.Close()
			delete(d.instances, id)
		}
	}
	return nil
}

func (d *Discoverer) wrappedReg(instanceId string) prometheus.Registerer {
	id := idWithRegion(aws.StringValue(d.awsSession.Config.Region), instanceId)
	return prometheus.WrapRegistererWith(prometheus.Labels{"rds_instance_id": id}, d.reg)
}

func idWithRegion(region, id string) string {
	if id == "" {
		return ""
	}
	if arn.IsARN(id) {
		a, _ := arn.Parse(id)
		region = a.Region
		id = a.Resource
		parts := strings.Split(a.Resource, ":")
		if len(parts) > 1 {
			id = parts[1]
		}
	}
	return region + "/" + id
}
