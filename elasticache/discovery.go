package elasticache

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/elasticache"
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
	api := elasticache.New(d.awsSession)

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

func (d *Discoverer) refresh(api *elasticache.ElastiCache) error {
	t := time.Now()
	defer func() {
		d.logger.Info("elasticache clusters refreshed in:", time.Since(t))
	}()

	var clusters []*elasticache.CacheCluster
	var err error

	for _, v := range []bool{false, true} {
		input := &elasticache.DescribeCacheClustersInput{}
		input.ShowCacheNodeInfo = aws.Bool(true)
		input.ShowCacheClustersNotInReplicationGroups = aws.Bool(v)
		for {
			output, err := api.DescribeCacheClusters(input)
			if err != nil {
				return err
			}
			for _, cluster := range output.CacheClusters {
				i := &elasticache.ListTagsForResourceInput{ResourceName: cluster.ARN}
				tags := map[string]string{}
				o, err := api.ListTagsForResource(i)
				if err != nil {
					d.logger.Error(err)
				} else {
					for _, t := range o.TagList {
						tags[aws.StringValue(t.Key)] = aws.StringValue(t.Value)
					}
				}
				if utils.Filtered(*flags.ElasticacheFilters, tags) {
					d.logger.Infof(
						"cluster %s (tags: %s) was skipped according to the tag-based filters: %s",
						aws.StringValue(cluster.CacheClusterId),
						tags,
						*flags.ElasticacheFilters,
					)
					continue
				}
				clusters = append(clusters, cluster)
			}
			if output.Marker != nil {
				input.SetMarker(aws.StringValue(output.Marker))
				continue
			}
			break
		}
	}

	actualInstances := map[string]bool{}
	for _, cluster := range clusters {
		for _, node := range cluster.CacheNodes {
			id := aws.StringValue(cluster.CacheClusterId) + "/" + aws.StringValue(node.CacheNodeId)
			actualInstances[id] = true
			i, ok := d.instances[id]
			if !ok {
				d.logger.Info("new Elasticache instance found:", id)
				i, err = NewCollector(d.awsSession, cluster, node)
				if err != nil {
					d.logger.Warning("failed to init Elasticache collector:", err)
					continue
				}
				if err := d.wrappedReg(id).Register(i); err != nil {
					d.logger.Warning(err)
					continue
				}
				d.instances[id] = i
			}
			i.update(cluster, node)
		}
	}

	for id, i := range d.instances {
		if !actualInstances[id] {
			d.logger.Info("Elasticache instance no longer exists:", id)
			d.wrappedReg(id).Unregister(i)
			i.Close()
			delete(d.instances, id)
		}
	}
	return nil
}

func (d *Discoverer) wrappedReg(instanceId string) prometheus.Registerer {
	id := utils.IdWithRegion(aws.StringValue(d.awsSession.Config.Region), instanceId)
	return prometheus.WrapRegistererWith(prometheus.Labels{"ec_instance_id": id}, d.reg)
}
