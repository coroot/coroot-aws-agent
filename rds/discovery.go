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
	"sync"
)

type Discoverer struct {
    reg         prometheus.Registerer
    awsSession  *session.Session
    instances   sync.Map
    logger      logger.Logger
    workerPool  *utils.WorkerPool
}

func NewDiscoverer(reg prometheus.Registerer, awsSession *session.Session) *Discoverer {
    return &Discoverer{
        reg:        reg,
        awsSession: awsSession,
        instances:  sync.Map{},
        logger:     logger.NewKlog(""),
        workerPool: utils.NewWorkerPool(10), // Adjust the number of workers as needed
    }
}

func (d *Discoverer) Run() {
    api := rds.New(d.awsSession)

    if err := d.refresh(api); err != nil {
        d.logger.Warning(err)
    }

    ticker := time.NewTicker(*flags.DiscoveryInterval)
    defer ticker.Stop()

    for range ticker.C {
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

    actualInstances := make(map[string]bool)
    var wg sync.WaitGroup
    for _, dbInstance := range output.DBInstances {
        wg.Add(1)
        d.workerPool.Submit(func() {
            defer wg.Done()
            id := aws.StringValue(dbInstance.DBInstanceIdentifier)
            actualInstances[id] = true
            d.processInstance(api, dbInstance)
        })
    }
    wg.Wait()

    d.instances.Range(func(key, value interface{}) bool {
        id := key.(string)
        if _, exists := actualInstances[id]; !exists {
            d.logger.Info("instance no longer exists:", id)
            d.wrappedReg(id).Unregister(value.(*Collector))
            value.(*Collector).Close()
            d.instances.Delete(id)
        }
        return true
    })

    return nil
}

func (d *Discoverer) processInstance(api rdsiface.RDSAPI, dbInstance *rds.DBInstance) {
    if dbInstance.Endpoint == nil {
        return
    }
    id := aws.StringValue(dbInstance.DBInstanceIdentifier)
    tags := d.getTags(api, dbInstance.DBInstanceArn)
    if utils.Filtered(*flags.RdsFilters, tags) {
        d.logger.Infof("RDS instance %s (tags: %s) was skipped according to the tag-based filters: %s", id, tags, *flags.RdsFilters)
        return
    }

    value, ok := d.instances.Load(id)
    if !ok {
        d.logger.Info("new DB instance found:", id)
        collector, err := NewCollector(d.awsSession, dbInstance)
        if err != nil {
            d.logger.Warning("failed to init RDS collector:", err)
            return
        }
        if err := d.wrappedReg(id).Register(collector); err != nil {
            d.logger.Warning(err)
            return
        }
        d.instances.Store(id, collector)
    } else {
        value.(*Collector).update(dbInstance)
    }
}

func (d *Discoverer) getTags(api rdsiface.RDSAPI, arn *string) map[string]string {
    input := &rds.ListTagsForResourceInput{ResourceName: arn}
    tags := map[string]string{}
    o, err := api.ListTagsForResource(input)
    if err != nil {
        d.logger.Error(err)
    } else {
        for _, t := range o.TagList {
            tags[aws.StringValue(t.Key)] = aws.StringValue(t.Value)
        }
    }
    return tags
}

func (d *Discoverer) wrappedReg(instanceId string) prometheus.Registerer {
	id := utils.IdWithRegion(aws.StringValue(d.awsSession.Config.Region), instanceId)
	return prometheus.WrapRegistererWith(prometheus.Labels{"rds_instance_id": id}, d.reg)
}
