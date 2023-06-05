package elasticache

import (
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/elasticache"
	"github.com/coroot/coroot-aws-agent/flags"
	"github.com/coroot/coroot-aws-agent/utils"
	"github.com/coroot/logger"
	"github.com/oliver006/redis_exporter/exporter"
	"github.com/prometheus/client_golang/prometheus"
	mcExporter "github.com/prometheus/memcached_exporter/pkg/exporter"
	"net"
	"strconv"
	"time"
)

var (
	dInfo = utils.Desc("aws_elasticache_info", "Elasticache instance info",
		"region", "availability_zone", "endpoint", "ipv4", "port",
		"engine", "engine_version", "instance_type", "cluster_id",
	)
	dStatus = utils.Desc("aws_elasticache_status", "Status of the Elasticache instance", "status")
)

type Collector struct {
	sess *session.Session

	metricCollector prometheus.Collector
	cluster         elasticache.CacheCluster
	node            elasticache.CacheNode

	logger logger.Logger
}

func NewCollector(sess *session.Session, cluster *elasticache.CacheCluster, node *elasticache.CacheNode) (*Collector, error) {
	if node.Endpoint == nil || node.Endpoint.Address == nil {
		return nil, fmt.Errorf("endpoint is not defined")
	}
	c := &Collector{
		sess:    sess,
		cluster: *cluster,
		node:    *node,
		logger:  logger.NewKlog(aws.StringValue(cluster.CacheClusterId)),
	}

	c.startMetricCollector()
	return c, nil
}

func (c *Collector) update(cluster *elasticache.CacheCluster, n *elasticache.CacheNode) {
	if aws.Int64Value(c.node.Endpoint.Port) != aws.Int64Value(n.Endpoint.Port) || aws.StringValue(c.node.Endpoint.Address) != aws.StringValue(n.Endpoint.Address) {
		c.cluster = *cluster
		c.node = *n
		c.startMetricCollector()
	}
	c.cluster = *cluster
	c.node = *n
}

func (c *Collector) startMetricCollector() {
	switch aws.StringValue(c.cluster.Engine) {
	case "redis":
		url := fmt.Sprintf("redis://%s:%d", aws.StringValue(c.node.Endpoint.Address), aws.Int64Value(c.node.Endpoint.Port))
		opts := exporter.Options{
			Namespace:          "redis",
			ConfigCommandName:  "CONFIG",
			IsCluster:          false,
			ConnectionTimeouts: *flags.ElasticacheConnectTimeout,
			RedisMetricsOnly:   true,
		}
		if collector, err := exporter.NewRedisExporter(url, opts); err != nil {
			c.logger.Warning("failed to init redis collector:", err)
		} else {
			c.logger.Info("redis collector ->", url)
			c.metricCollector = collector
		}
	case "memcached":
		address := fmt.Sprintf("%s:%d", aws.StringValue(c.node.Endpoint.Address), aws.Int64Value(c.node.Endpoint.Port))
		c.metricCollector = mcExporter.New(
			address,
			*flags.ElasticacheConnectTimeout,
			&promLogger{c.logger},
			nil,
		)
		c.logger.Info("memcached collector ->", address)
	}
}

func (c *Collector) Close() {}

func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	ch <- utils.Gauge(dStatus, 1, aws.StringValue(c.node.CacheNodeStatus))

	var ip string
	if a, err := net.ResolveIPAddr("", aws.StringValue(c.node.Endpoint.Address)); err != nil {
		c.logger.Warning(err)
	} else {
		ip = a.String()
	}

	cluster := aws.StringValue(c.cluster.ReplicationGroupId)
	if cluster == "" {
		cluster = aws.StringValue(c.cluster.CacheClusterId)
	}

	ch <- utils.Gauge(dInfo, 1,
		aws.StringValue(c.sess.Config.Region),
		aws.StringValue(c.node.CustomerAvailabilityZone),
		aws.StringValue(c.node.Endpoint.Address),
		ip,
		strconv.Itoa(int(aws.Int64Value(c.node.Endpoint.Port))),
		aws.StringValue(c.cluster.Engine),
		aws.StringValue(c.cluster.EngineVersion),
		aws.StringValue(c.cluster.CacheNodeType),
		cluster,
	)

	if c.metricCollector != nil {
		t := time.Now()
		c.metricCollector.Collect(ch)
		c.logger.Info("cache metrics collected in:", time.Since(t))
	}
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	ch <- dInfo
	ch <- dStatus
}

type promLogger struct {
	l logger.Logger
}

func (l *promLogger) Log(keyvals ...interface{}) error {
	l.l.Info(keyvals...)
	return nil
}
