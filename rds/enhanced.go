package rds

import (
	"encoding/json"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/cloudwatchlogs"
	"github.com/coroot/coroot-aws-agent/utils"
	"github.com/prometheus/client_golang/prometheus"
)

const rdsMetricsLogGroupName = "RDSOSMetrics"

func (c *Collector) collectOsMetrics(ch chan<- prometheus.Metric) {
	input := cloudwatchlogs.GetLogEventsInput{
		Limit:         aws.Int64(1),
		StartFromHead: aws.Bool(false),
		LogGroupName:  aws.String(rdsMetricsLogGroupName),
		LogStreamName: c.instance.DbiResourceId,
	}
	out, err := c.cloudWatchLogsApi.GetLogEvents(&input)
	if err != nil {
		c.logger.Warningf("failed to read log stream %s:%s: %s", rdsMetricsLogGroupName, aws.StringValue(c.instance.DbiResourceId), err)
		return
	}
	if len(out.Events) < 1 {
		return
	}
	var m osMetrics
	if err := json.Unmarshal([]byte(*out.Events[0].Message), &m); err != nil {
		c.logger.Warning("failed to parse enhanced monitoring data:", err)
		return
	}
	ch <- utils.Gauge(dCPUCores, float64(m.NumVCPUs))
	ch <- utils.Gauge(dCpuUsage, m.Cpu.Guest, "guest")
	ch <- utils.Gauge(dCpuUsage, m.Cpu.Irq, "irq")
	ch <- utils.Gauge(dCpuUsage, m.Cpu.Nice, "nice")
	ch <- utils.Gauge(dCpuUsage, m.Cpu.Steal, "steal")
	ch <- utils.Gauge(dCpuUsage, m.Cpu.System, "system")
	ch <- utils.Gauge(dCpuUsage, m.Cpu.User, "user")
	ch <- utils.Gauge(dCpuUsage, m.Cpu.Wait, "wait")

	ch <- utils.Gauge(dMemTotal, float64(m.Memory.Total*1000))
	ch <- utils.Gauge(dMemCached, float64(m.Memory.Cached*1000))
	ch <- utils.Gauge(dMemFree, float64(m.Memory.Free*1000))

	for _, ioStat := range m.PhysicalDeviceIO {
		ch <- utils.Gauge(dIOps, ioStat.ReadIOsPS, ioStat.Device, "read")
		ch <- utils.Gauge(dIOps, ioStat.WriteIOsPS, ioStat.Device, "write")
		ch <- utils.Gauge(dIObytes, ioStat.ReadKbPS*1000, ioStat.Device, "read")
		ch <- utils.Gauge(dIObytes, ioStat.WriteKb*1000, ioStat.Device, "write")
		ch <- utils.Gauge(dIOawait, ioStat.Await/1000, ioStat.Device)
		ch <- utils.Gauge(dIOutil, ioStat.Util, ioStat.Device)
	}
	for _, dIO := range m.DiskIO {
		if dIO.Device == "" { // Aurora network disk
			device := "aurora-data"
			if dIO.ReadIOsPS != nil && dIO.WriteIOsPS != nil {
				ch <- utils.Gauge(dIOps, *dIO.ReadIOsPS, device, "read")
				ch <- utils.Gauge(dIOps, *dIO.WriteIOsPS, device, "write")
			}
			if dIO.ReadLatency != nil && dIO.WriteLatency != nil {
				ch <- utils.Gauge(dIOlatency, *dIO.ReadLatency/1000, device, "read")
				ch <- utils.Gauge(dIOlatency, *dIO.WriteLatency/1000, device, "write")
			}
		}
	}

	for _, fsStat := range m.FileSys {
		ch <- utils.Gauge(dFSTotal, float64(fsStat.Total*1000), fsStat.MountPoint)
		ch <- utils.Gauge(dFSUsed, float64(fsStat.Used*1000), fsStat.MountPoint)
	}
	for _, iface := range m.NetworkInterfaces {
		ch <- utils.Gauge(dNetRx, iface.Rx, iface.Interface)
		ch <- utils.Gauge(dNetTx, iface.Tx, iface.Interface)
	}
}

type osMetrics struct {
	NumVCPUs          int                `json:"numVCPUs"`
	Cpu               cpuUtilization     `json:"cpuUtilization"`
	Memory            rdsMemory          `json:"memory"`
	PhysicalDeviceIO  []physicalDeviceIO `json:"physicalDeviceIO"`
	DiskIO            []auroraDiskIO     `json:"diskIO"`
	FileSys           []fileSys          `json:"fileSys"`
	NetworkInterfaces []netInterface     `json:"network"`
}

type netInterface struct {
	Interface string  `json:"interface"`
	Rx        float64 `json:"rx"`
	Tx        float64 `json:"tx"`
}

type cpuUtilization struct {
	Guest  float64 `json:"guest"`
	Irq    float64 `json:"irq"`
	System float64 `json:"system"`
	Wait   float64 `json:"wait"`
	Idle   float64 `json:"idle"`
	User   float64 `json:"user"`
	Steal  float64 `json:"steal"`
	Nice   float64 `json:"nice"`
	Total  float64 `json:"total"`
}

type rdsMemory struct {
	Writeback      int64 `json:"writeback"`
	HugePagesFree  int64 `json:"hugePagesFree"`
	HugePagesRsvd  int64 `json:"hugePagesRsvd"`
	HugePagesSurp  int64 `json:"hugePagesSurp"`
	Cached         int64 `json:"cached"`
	HugePagesSize  int64 `json:"hugePagesSize"`
	Free           int64 `json:"free"`
	HugePagesTotal int64 `json:"hugePagesTotal"`
	Inactive       int64 `json:"inactive"`
	PageTables     int64 `json:"pageTables"`
	Dirty          int64 `json:"dirty"`
	Mapped         int64 `json:"mapped"`
	Active         int64 `json:"active"`
	Total          int64 `json:"total"`
	Slab           int64 `json:"slab"`
	Buffers        int64 `json:"buffers"`
}
type physicalDeviceIO struct {
	WriteKbPS   float64 `json:"writeKbPS"`
	ReadIOsPS   float64 `json:"readIOsPS"`
	Await       float64 `json:"await"`
	ReadKbPS    float64 `json:"readKbPS"`
	RrqmPS      float64 `json:"rrqmPS"`
	Util        float64 `json:"util"`
	AvgQueueLen float64 `json:"avgQueueLen"`
	Tps         float64 `json:"tps"`
	ReadKb      float64 `json:"readKb"`
	Device      string  `json:"device"`
	WriteKb     float64 `json:"writeKb"`
	AvgReqSz    float64 `json:"avgReqSz"`
	WrqmPS      float64 `json:"wrqmPS"`
	WriteIOsPS  float64 `json:"writeIOsPS"`
}

type auroraDiskIO struct {
	Device          string   `json:"device"`
	ReadLatency     *float64 `json:"readLatency"`
	WriteLatency    *float64 `json:"writeLatency"`
	WriteThroughput *float64 `json:"writeThroughput"`
	ReadThroughput  *float64 `json:"readThroughput"`
	ReadIOsPS       *float64 `json:"readIOsPS"`
	WriteIOsPS      *float64 `json:"writeIOsPS"`
	DiskQueueDepth  *float64 `json:"diskQueueDepth"`
}

type fileSys struct {
	MaxFiles   int64  `json:"maxFiles"`
	MountPoint string `json:"mountPoint"`
	Name       string `json:"name"`
	Total      int64  `json:"total"`
	Used       int64  `json:"used"`
	UsedFiles  int64  `json:"usedFiles"`
}
