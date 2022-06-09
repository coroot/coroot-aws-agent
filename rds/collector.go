package rds

import (
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go/service/rds"
	postgres "github.com/coroot/coroot-pg-agent/collector"
	"github.com/coroot/logger"
	"github.com/coroot/logparser"
	"github.com/prometheus/client_golang/prometheus"
	"net"
	"net/url"
	"strconv"
	"sync"
	"time"
)

var (
	dInfo = desc("aws_rds_info", "RDS instance info",
		"region", "availability_zone", "endpoint", "ipv4", "port",
		"engine", "engine_version", "instance_type", "storage_type",
		"secondary_availability_zone", "cluster_id", "source_instance_id",
	)
	dStatus                      = desc("aws_rds_status", "Status of the RDS instance", "status")
	dAllocatedStorage            = desc("aws_rds_allocated_storage_gibibytes", "Allocated storage size")
	dStorageAutoscalingThreshold = desc("aws_rds_storage_autoscaling_threshold_gibibytes", "Storage autoscaling threshold")
	dStorageProvisionedIOPs      = desc("aws_rds_storage_provisioned_iops", "Number of provisioned IOPs")
	dReadReplicaInfo             = desc("aws_rds_read_replica_info", "Read replica info", "replica_instance_id")
	dBackupRetentionPeriod       = desc("aws_rds_backup_retention_period_days", "Backup retention period")

	dCPUCores  = desc("aws_rds_cpu_cores", "The number of virtual CPUs")
	dCpuUsage  = desc("aws_rds_cpu_usage_percent", "The percentage of the CPU spent in each mode", "mode")
	dIOps      = desc("aws_rds_io_ops_per_second", "The number of I/O transactions per second", "device", "operation")
	dIObytes   = desc("aws_rds_io_bytes_per_second", "The number of bytes read or written per second", "device", "operation")
	dIOawait   = desc("aws_rds_io_await_seconds", "The number of seconds required to respond to requests, including queue time and service time", "device")
	dIOutil    = desc("aws_rds_io_util_percent", "The percentage of CPU time during which requests were issued.", "device")
	dFSTotal   = desc("aws_rds_fs_total_bytes", "The total number of disk space available for the file system", "mount_point")
	dFSUsed    = desc("aws_rds_fs_used_bytes", "The amount of disk space used by files in the file system", "mount_point")
	dMemTotal  = desc("aws_rds_memory_total_bytes", "The total amount of memory")
	dMemCached = desc("aws_rds_memory_cached_bytes", "The amount of memory used as page cache")
	dMemFree   = desc("aws_rds_memory_free_bytes", "The amount of unassigned memory")
	dNetRx     = desc("aws_rds_net_rx_bytes_per_second", "The number of bytes received per second", "interface")
	dNetTx     = desc("aws_rds_net_tx_bytes_per_second", "The number of bytes transmitted per second", "interface")

	dLogMessages = desc("aws_rds_log_messages_total",
		"Number of messages grouped by the automatically extracted repeated pattern",
		"level", "pattern_hash", "sample")
)

type DbCollectorConf struct {
	User           string
	Password       string
	ConnectTimeout time.Duration
	QueryTimeout   time.Duration

	LogsScrapeInterval time.Duration
	DbScrapeInterval   time.Duration
}

type DbCollector interface {
	prometheus.Collector
	Close() error
}

type Collector struct {
	sess     *session.Session
	region   string
	dbConf   DbCollectorConf
	instance rds.DBInstance

	cloudWatchLogsApi *cloudwatchlogs.CloudWatchLogs

	dbCollector DbCollector

	logReader *LogReader
	logParser *logparser.Parser

	logger logger.Logger
}

func NewCollector(sess *session.Session, i *rds.DBInstance, dbConf DbCollectorConf) (*Collector, error) {
	c := &Collector{
		sess:              sess,
		region:            aws.StringValue(sess.Config.Region),
		dbConf:            dbConf,
		instance:          *i,
		cloudWatchLogsApi: cloudwatchlogs.New(sess),
		logger:            logger.NewKlog(aws.StringValue(i.DBInstanceIdentifier)),
	}

	c.startDbCollector()
	c.startLogCollector()

	return c, nil
}

func (c *Collector) update(i *rds.DBInstance) {
	if i == nil {
		return
	}
	ci := c.instance
	if aws.Int64Value(i.Endpoint.Port) != aws.Int64Value(ci.Endpoint.Port) || aws.StringValue(i.Endpoint.Address) != aws.StringValue(ci.Endpoint.Address) {
		_ = c.dbCollector.Close()
		c.instance = *i
		c.startDbCollector()
	}
	c.instance = *i
}

func (c *Collector) startDbCollector() {
	if c.dbCollector != nil {
		_ = c.dbCollector.Close()
	}
	i := c.instance
	switch aws.StringValue(i.Engine) {
	case "postgres", "aurora-postgresql":
		endpoint := net.JoinHostPort(aws.StringValue(i.Endpoint.Address), strconv.Itoa(int(aws.Int64Value(i.Endpoint.Port))))
		userPass := url.UserPassword(c.dbConf.User, c.dbConf.Password)
		connectTimeout := int(c.dbConf.ConnectTimeout.Seconds())
		if connectTimeout < 1 {
			connectTimeout = 1
		}
		statementTimeout := int(c.dbConf.QueryTimeout.Milliseconds())
		dsn := fmt.Sprintf("postgresql://%s@%s/postgres?connect_timeout=%d&statement_timeout=%d", userPass, endpoint, connectTimeout, statementTimeout)
		if collector, err := postgres.New(dsn, c.dbConf.DbScrapeInterval, c.logger); err != nil {
			c.logger.Warning("failed to init postgres collector:", err)
		} else {
			c.logger.Info("started postgres collector:", endpoint)
			c.dbCollector = collector
		}
	}
}

func (c *Collector) startLogCollector() {
	if c.dbConf.LogsScrapeInterval <= 0 {
		return
	}
	switch aws.StringValue(c.instance.Engine) {
	case "postgres", "aurora-postgresql":
		ch := make(chan logparser.LogEntry)
		c.logParser = logparser.NewParser(ch, nil)
		c.logReader = NewLogReader(rds.New(c.sess), c.instance.DBInstanceIdentifier, ch, c.dbConf.LogsScrapeInterval, c.logger)
	}
}

func (c *Collector) Close() {
	if c.dbCollector != nil {
		_ = c.dbCollector.Close()
	}
	if c.logReader != nil {
		c.logReader.Stop()
	}
	if c.logParser != nil {
		c.logParser.Stop()
	}
}

func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	i := c.instance

	ch <- gauge(dStatus, 1, aws.StringValue(i.DBInstanceStatus))

	var ip string
	if a, err := net.ResolveIPAddr("", aws.StringValue(i.Endpoint.Address)); err != nil {
		c.logger.Warning(err)
	} else {
		ip = a.String()
	}

	ch <- gauge(dInfo, 1,
		c.region,
		aws.StringValue(i.AvailabilityZone),

		aws.StringValue(i.Endpoint.Address),
		ip,
		strconv.Itoa(int(aws.Int64Value(i.Endpoint.Port))),

		aws.StringValue(i.Engine),
		aws.StringValue(i.EngineVersion),

		aws.StringValue(i.DBInstanceClass),
		aws.StringValue(i.StorageType),

		aws.StringValue(i.SecondaryAvailabilityZone),

		idWithRegion(c.region, aws.StringValue(i.DBClusterIdentifier)),

		idWithRegion(c.region, aws.StringValue(i.ReadReplicaSourceDBInstanceIdentifier)),
	)

	ch <- gauge(dAllocatedStorage, float64(aws.Int64Value(i.AllocatedStorage)))
	ch <- gauge(dStorageAutoscalingThreshold, float64(aws.Int64Value(i.MaxAllocatedStorage)))
	ch <- gauge(dStorageProvisionedIOPs, float64(aws.Int64Value(i.Iops)))
	ch <- gauge(dBackupRetentionPeriod, float64(aws.Int64Value(i.BackupRetentionPeriod)))

	for _, r := range i.ReadReplicaDBInstanceIdentifiers {
		ch <- gauge(dReadReplicaInfo, float64(1), idWithRegion(c.region, aws.StringValue(r)))
	}

	wg := sync.WaitGroup{}

	if aws.Int64Value(c.instance.MonitoringInterval) > 0 && c.instance.DbiResourceId != nil {
		wg.Add(1)
		go func() {
			t := time.Now()
			c.collectOsMetrics(ch)
			c.logger.Info("os metrics collected in:", time.Since(t))
			wg.Done()
		}()
	}

	if c.dbCollector != nil {
		wg.Add(1)
		go func() {
			t := time.Now()
			c.dbCollector.Collect(ch)
			c.logger.Info("db metrics collected in:", time.Since(t))
			wg.Done()
		}()
	}

	wg.Wait()

	if c.logParser != nil {
		for _, lc := range c.logParser.GetCounters() {
			ch <- counter(dLogMessages, float64(lc.Messages), lc.Level.String(), lc.Hash, lc.Sample)
		}
	}
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	ch <- dInfo
	ch <- dStatus
	ch <- dAllocatedStorage
	ch <- dCPUCores
	ch <- dCpuUsage
	ch <- dMemTotal
	ch <- dMemCached
	ch <- dMemFree
	ch <- dIOps
	ch <- dIObytes
	ch <- dIOutil
	ch <- dIOawait
	ch <- dFSTotal
	ch <- dFSUsed
	ch <- dNetRx
	ch <- dNetTx
	ch <- dLogMessages
	if c.dbCollector != nil {
		c.dbCollector.Describe(ch)
	}
}

func desc(name, help string, labels ...string) *prometheus.Desc {
	return prometheus.NewDesc(name, help, labels, nil)
}

func gauge(desc *prometheus.Desc, value float64, labels ...string) prometheus.Metric {
	return prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, value, labels...)
}

func counter(desc *prometheus.Desc, value float64, labels ...string) prometheus.Metric {
	return prometheus.MustNewConstMetric(desc, prometheus.CounterValue, value, labels...)
}
