package rds

import (
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go/service/rds"
	"github.com/coroot/coroot-aws-agent/flags"
	"github.com/coroot/coroot-aws-agent/utils"
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
	dInfo = utils.Desc("aws_rds_info", "RDS instance info",
		"region", "availability_zone", "endpoint", "ipv4", "port",
		"engine", "engine_version", "instance_type", "storage_type",
		"secondary_availability_zone", "cluster_id", "source_instance_id",
	)
	dStatus                      = utils.Desc("aws_rds_status", "Status of the RDS instance", "status")
	dAllocatedStorage            = utils.Desc("aws_rds_allocated_storage_gibibytes", "Allocated storage size")
	dStorageAutoscalingThreshold = utils.Desc("aws_rds_storage_autoscaling_threshold_gibibytes", "Storage autoscaling threshold")
	dStorageProvisionedIOPs      = utils.Desc("aws_rds_storage_provisioned_iops", "Number of provisioned IOPs")
	dReadReplicaInfo             = utils.Desc("aws_rds_read_replica_info", "Read replica info", "replica_instance_id")
	dBackupRetentionPeriod       = utils.Desc("aws_rds_backup_retention_period_days", "Backup retention period")

	dCPUCores  = utils.Desc("aws_rds_cpu_cores", "The number of virtual CPUs")
	dCpuUsage  = utils.Desc("aws_rds_cpu_usage_percent", "The percentage of the CPU spent in each mode", "mode")
	dIOps      = utils.Desc("aws_rds_io_ops_per_second", "The number of I/O transactions per second", "device", "operation")
	dIObytes   = utils.Desc("aws_rds_io_bytes_per_second", "The number of bytes read or written per second", "device", "operation")
	dIOlatency = utils.Desc("aws_rds_io_latency_seconds", "The average elapsed time between the submission of an I/O request and its completion (Amazon Aurora only)", "device", "operation")
	dIOawait   = utils.Desc("aws_rds_io_await_seconds", "The number of seconds required to respond to requests, including queue time and service time", "device")
	dIOutil    = utils.Desc("aws_rds_io_util_percent", "The percentage of CPU time during which requests were issued.", "device")
	dFSTotal   = utils.Desc("aws_rds_fs_total_bytes", "The total number of disk space available for the file system", "mount_point")
	dFSUsed    = utils.Desc("aws_rds_fs_used_bytes", "The amount of disk space used by files in the file system", "mount_point")
	dMemTotal  = utils.Desc("aws_rds_memory_total_bytes", "The total amount of memory")
	dMemCached = utils.Desc("aws_rds_memory_cached_bytes", "The amount of memory used as page cache")
	dMemFree   = utils.Desc("aws_rds_memory_free_bytes", "The amount of unassigned memory")
	dNetRx     = utils.Desc("aws_rds_net_rx_bytes_per_second", "The number of bytes received per second", "interface")
	dNetTx     = utils.Desc("aws_rds_net_tx_bytes_per_second", "The number of bytes transmitted per second", "interface")

	dLogMessages = utils.Desc("aws_rds_log_messages_total",
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
	instance rds.DBInstance

	cloudWatchLogsApi *cloudwatchlogs.CloudWatchLogs

	dbCollector DbCollector

	logReader *LogReader
	logParser *logparser.Parser

	logger logger.Logger
}

func NewCollector(sess *session.Session, i *rds.DBInstance) (*Collector, error) {
	c := &Collector{
		sess:              sess,
		region:            aws.StringValue(sess.Config.Region),
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
		userPass := url.UserPassword(*flags.RdsDbUser, *flags.RdsDbPassword)
		connectTimeout := int((*flags.RdsDbConnectTimeout).Seconds())
		if connectTimeout < 1 {
			connectTimeout = 1
		}
		statementTimeout := int((*flags.RdsDbQueryTimeout).Milliseconds())
		dsn := fmt.Sprintf("postgresql://%s@%s/postgres?connect_timeout=%d&statement_timeout=%d", userPass, endpoint, connectTimeout, statementTimeout)
		if collector, err := postgres.New(dsn, *flags.DbScrapeInterval, c.logger); err != nil {
			c.logger.Warning("failed to init postgres collector:", err)
		} else {
			c.logger.Info("started postgres collector:", endpoint)
			c.dbCollector = collector
		}
	}
}

func (c *Collector) startLogCollector() {
	if *flags.RdsLogsScrapeInterval <= 0 {
		return
	}
	switch aws.StringValue(c.instance.Engine) {
	case "postgres", "aurora-postgresql":
		ch := make(chan logparser.LogEntry)
		c.logParser = logparser.NewParser(ch, nil)
		c.logReader = NewLogReader(rds.New(c.sess), c.instance.DBInstanceIdentifier, ch, *flags.RdsLogsScrapeInterval, c.logger)
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

	ch <- utils.Gauge(dStatus, 1, aws.StringValue(i.DBInstanceStatus))

	var ip string
	if a, err := net.ResolveIPAddr("", aws.StringValue(i.Endpoint.Address)); err != nil {
		c.logger.Warning(err)
	} else {
		ip = a.String()
	}

	ch <- utils.Gauge(dInfo, 1,
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

		utils.IdWithRegion(c.region, aws.StringValue(i.DBClusterIdentifier)),

		utils.IdWithRegion(c.region, aws.StringValue(i.ReadReplicaSourceDBInstanceIdentifier)),
	)

	ch <- utils.Gauge(dAllocatedStorage, float64(aws.Int64Value(i.AllocatedStorage)))
	ch <- utils.Gauge(dStorageAutoscalingThreshold, float64(aws.Int64Value(i.MaxAllocatedStorage)))
	ch <- utils.Gauge(dStorageProvisionedIOPs, float64(aws.Int64Value(i.Iops)))
	ch <- utils.Gauge(dBackupRetentionPeriod, float64(aws.Int64Value(i.BackupRetentionPeriod)))

	for _, r := range i.ReadReplicaDBInstanceIdentifiers {
		ch <- utils.Gauge(dReadReplicaInfo, float64(1), utils.IdWithRegion(c.region, aws.StringValue(r)))
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
			ch <- utils.Counter(dLogMessages, float64(lc.Messages), lc.Level.String(), lc.Hash, lc.Sample)
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
	ch <- dIOlatency
	ch <- dIOps
	ch <- dIObytes
	ch <- dIOutil
	ch <- dIOawait
	ch <- dFSTotal
	ch <- dFSUsed
	ch <- dNetRx
	ch <- dNetTx
	ch <- dLogMessages
}
