package rds

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/rds"
	"github.com/aws/aws-sdk-go/service/rds/rdsiface"
	"github.com/coroot/coroot-aws-agent/flags"
	"github.com/coroot/coroot-aws-agent/utils"
	"github.com/coroot/logger"
	"github.com/prometheus/client_golang/prometheus"
	"time"
)

type Discoverer struct {
	reg prometheus.Registerer

	awsSession *session.Session

	instances map[string]*Collector

	logger logger.Logger
}

func NewDiscoverer(reg prometheus.Registerer, awsSession *session.Session) *Discoverer {
	d := &Discoverer{
		reg:        reg,
		awsSession: awsSession,
		instances:  map[string]*Collector{},
		logger:     logger.NewKlog(""),
	}
	return d
}

func (d *Discoverer) Run() {
	api := rds.New(d.awsSession)

	if err := d.refresh(api); err != nil {
		d.logger.Warning(err)
	}

	ticker := time.Tick(*flags.DiscoveryInterval)
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
			i, err = NewCollector(d.awsSession, dbInstance)
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
	id := utils.IdWithRegion(aws.StringValue(d.awsSession.Config.Region), instanceId)
	return prometheus.WrapRegistererWith(prometheus.Labels{"rds_instance_id": id}, d.reg)
}
