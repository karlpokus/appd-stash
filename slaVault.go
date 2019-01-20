package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/influxdata/influxdb/client/v2"
	//"github.com/influxdata/influxdb1-client/v2"
)

// DataSource Configuration
type DataSource struct {
	UniqueName string `json:"unique_name"` // used as tag to differenciate metrics rom differennt environments: prod, perf
	Host       string `json:"host"`        // AppDynamics host:port
	MetricPath string `json:"metric_path"` // Path to BT metrics
	RestUser   string `json:"rest_user"`
	RestPwd    string `json:"rest_pwd"`
}

// Configuration
type conf struct {
	// TODO: need support for serveral data sources
	Database struct {
		Host   string `json:"db_host"`
		DbName string `json:"db_name"` // Name of influxDB <-- needs to be created and configured
		DBUser string `json:"db_user"`
		DbPwd  string `json:"db_pwd"`
	} `json:"database"`
	DataSources []DataSource `json:"data_sources"`
}

// BT metric names for SLA calculation
const calls = "Calls per Minute"
const errors = "Errors per Minute"
const stalls = "Stall Count"
const veryslow = "Number of Very Slow Calls"

// SLA object with methods
type SLA struct {
	startTime     int64 // epoch
	frequency     string
	availability  float64
	performance   float64
	totalCalls    int
	totalErrors   int
	totalStalls   int
	totalVerySlow int
}

//CalcSLA Calculate SLA and set availability & performance fields
func (s *SLA) CalcSLA() {

	if 0 < s.totalCalls {
		// percent that affects availability
		a := 100 * float64((s.totalErrors + s.totalStalls)) / float64(s.totalCalls)
		// Percent that affects performande
		p := 100 * (float64(s.totalVerySlow) / float64(s.totalCalls))

		s.availability = float64(100) - a
		s.performance = float64(100) + p
	}

}

func (s *SLA) setCalls(v int) {
	s.totalCalls = v
}

func (s *SLA) setErrors(v int) {
	s.totalErrors = v
}

func (s *SLA) setStalls(v int) {
	s.totalStalls = v
}

func (s *SLA) setVerySlow(v int) {
	s.totalVerySlow = v
}

// MetricValues - The values
type MetricValues struct {
	Occurrences       int   `json:"-"` //skip
	Current           int   `json:"-"`
	Min               int   `json:"-"`
	Max               int   `json:"-"`
	StartTimeInMillis int64 `json:"startTimeInMillis"`
	UseRange          bool  `json:"-"`
	Count             int   `json:"-"`
	Sum               int   `json:"sum"`
	Value             int   `json:"-"`
	StandardDeviation int   `json:"-"`
}

// Metrics - Metric info
type Metrics struct {
	MetricName string         `json:"-"`
	MetricID   int            `json:"-"`
	MetricPath string         `json:"metricPath"`
	Frequency  string         `json:"frequency"`
	Values     []MetricValues `json:"metricValues"` //Array of metric values
}

// BTmetrics - Business Transaction Performance metrics from AppDynamics - JSON format
type BTmetrics struct {
	BTmetrics []Metrics `json:"metrics"` // Array of metrics
}

// isEmpty - Method for "METRIC DATA NOT FOUND"
// or MetricValues are empty
func (md Metrics) isEmpty() bool {

	const noData = "METRIC DATA NOT FOUND"
	bEmpty := false

	if 0 == strings.Compare(md.MetricName, noData) {
		bEmpty = true
	} else if 0 == len(md.Values) {
		bEmpty = true
	}
	return bEmpty
}

// fixJSONFormat - fix JSON format
func fixJSONFormat(toFix []byte) []byte {

	// Prepend header
	fix := append([]byte("{ \"metrics\": "), toFix...)
	// Append paranthesis
	fix = append(fix, "}"...)

	return fix
}

// Get minute scorecard data from AppD Controller ( DataSource = ds)
func getJSONData(ds int, minutes int, rolledUp bool) []byte {
	// Ignore all temporal ds connection errors -> the show must go on!
	// Log the error
	defer func() {
		if err := recover(); err != nil {
			log.Printf("getJSONdata ds=%v error = %v\n", ds, err)
		}
	}()

	// get config
	c := getConfig()

	// build raw URL
	raw := fmt.Sprintf("%v/metric-data?metric-path=%v&time-range-type=BEFORE_NOW&duration-in-mins=%v&output=JSON&rollup=%v", c.DataSources[ds].Host, c.DataSources[ds].MetricPath, minutes, rolledUp)

	// validate and encode URL
	url, _ := url.Parse(raw)
	q := url.Query()
	url.RawQuery = q.Encode()

	//fmt.Println(url)

	req, _ := http.NewRequest("GET", url.String(), nil)

	req.SetBasicAuth(c.DataSources[ds].RestUser, c.DataSources[ds].RestPwd)

	var bodyBytes []byte

	// Don't panic!  Thread must run even if no contact with data source at the moment
	// return empty response body on faulure
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return bodyBytes
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		bodyBytes, _ = ioutil.ReadAll(resp.Body)
	}
	// Fix JSON format before returning it
	return fixJSONFormat(bodyBytes)
}

func getSLA(ds int, minBeforeNow int, bRolledup bool) map[int64]SLA {

	// Get SLA data from Controller
	fixedValue := getJSONData(ds, minBeforeNow, bRolledup)

	var metrics BTmetrics

	// unmarshal byteArray into BTMetrics struct
	json.Unmarshal(fixedValue, &metrics)

	//	fmt.Printf("%v+", metrics)

	mSeries := make(map[int64]SLA)

	// Loop over all metrics
	for bt := 0; bt < len(metrics.BTmetrics); bt++ {
		// Build map startTime|SLA-struct
		if 0 == len(metrics.BTmetrics[bt].Values) {
			//Series are empty - > do nothing
			continue
		} else {
			// Loop over all metric values and add values to map
			for i := 0; i < len(metrics.BTmetrics); i++ {

				// Ignore empty metric type
				if true == metrics.BTmetrics[i].isEmpty() {
					continue
				}
				// The values series for a specific metric
				v := metrics.BTmetrics[i].Values

				// Last position is the metric name
				// Example: "Business Transaction Performance|Business Transaction Groups|SLA|Errors per Minute"
				s := strings.Split(metrics.BTmetrics[i].MetricPath, "|")
				m := len(s) - 1

				// straighten out the loop to avoid many string compares
				sw := 0
				if 0 == strings.Compare(calls, s[m]) {
					sw = 1 // totalCalls
				} else if 0 == strings.Compare(errors, s[m]) {
					sw = 2 // totalErrors
				} else if 0 == strings.Compare(stalls, s[m]) {
					sw = 3 // totalStalls
				} else if 0 == strings.Compare(veryslow, s[m]) {
					sw = 4 // totalVerySlow
				}

				// Set values in SLA object according to the correct name
				for j := 0; j < len(v); j++ {
					// Check if  SLA object already exists if not create a new
					var sla SLA
					_, exists := mSeries[v[j].StartTimeInMillis]
					if false == exists {
						mSeries[v[j].StartTimeInMillis] = SLA{v[j].StartTimeInMillis, metrics.BTmetrics[i].Frequency, 100, 100, 0, 0, 0, 0}
					}
					// Pointer to the SLA object in the map
					sla, _ = mSeries[v[j].StartTimeInMillis]

					switch sw {
					case 1:
						sla.setCalls(v[j].Sum)
					case 2:
						sla.setErrors(v[j].Sum)
					case 3:
						sla.setStalls(v[j].Sum)
					case 4:
						sla.setVerySlow(v[j].Sum)
					}
					mSeries[v[j].StartTimeInMillis] = sla
				}
			}
		}
	}

	// Calculate SLA
	for k := range mSeries {
		var sla SLA
		sla = mSeries[k]

		sla.CalcSLA()

		mSeries[k] = sla
	}

	//fmt.Printf("%v+\n", mSeries)

	return mSeries
}

func printSLA(series map[int64]SLA) {

	// no default support for sorting int64, convert key to int and build a sorted iterator
	sortedKeys := make([]int, 0, len(series))
	for k := range series {
		sortedKeys = append(sortedKeys, int(k))
	}
	sort.Ints(sortedKeys)

	// print the sorted metric series map using the sorted iterator
	for _, k := range sortedKeys {
		s := series[int64(k)]
		t := s.startTime / 1000 // millis to seconds
		//fmt.Printf("HIS_%v: %v, A = %3.1f, P = %3.1f\n", s.frequency, time.Unix(t, 0), s.availability, s.performance)
		fmt.Printf("HIS_%v: %v, A = %3.1f, P = %3.1f \n%+v\n", s.frequency, time.Unix(t, 0), s.availability, s.performance, s)
	}

	fmt.Printf("Series lenght = %v\n", len(sortedKeys))

}

func influxSaveData(ds int, series map[int64]SLA) {
	// Ignore all temporal InfluxDB connection problems
	// We have a 4 hour window to save minute data-> the show must go on!
	// Log the error
	defer func() {
		if err := recover(); err != nil {
			log.Printf("influxSaveData ds=%v error = %v\n", ds, err)
		}
	}()

	// Don't try to save if series are empty
	if 0 == len(series) {
		return
	}

	// Get Configuration
	cnf := getConfig()

	// Create a new HTTPClient
	c, _ := client.NewHTTPClient(client.HTTPConfig{
		Addr:     cnf.Database.Host,
		Username: cnf.Database.DBUser,
		Password: cnf.Database.DbPwd,
	})
	defer c.Close()

	// Create a new point batch
	bp, _ := client.NewBatchPoints(client.BatchPointsConfig{
		Database: cnf.Database.DbName,
	})

	// Tags: Database name, environment name
	tags := map[string]string{cnf.Database.DbName: cnf.DataSources[ds].UniqueName}
	for _, v := range series {
		fields := map[string]interface{}{
			"availability":  v.availability,
			"performnance":  v.performance,
			"totalCalls":    v.totalCalls,
			"totalErrors":   v.totalErrors,
			"totalStalls":   v.totalStalls,
			"totalVerySlow": v.totalVerySlow,
		}

		t := v.startTime / 1000
		pt, _ := client.NewPoint(cnf.Database.DbName, tags, fields, time.Unix(t, 0))

		bp.AddPoint(pt)
	}
	//  Write batch
	c.Write(bp)

	// Close client resources
	c.Close()
}

func influxSaveTimings(ds int, slen int, start time.Time, get time.Duration, save time.Duration) {
	// Ignore all temporal InfluxDB connection problems
	// We have a 4 hour window to save minute data-> the show must go on!
	// Log the error
	defer func() {
		if err := recover(); err != nil {
			log.Printf("influxSaveTimings ds=%v error = %v\n", ds, err)
		}
	}()

	// Get Configuration
	cnf := getConfig()

	// Create a new HTTPClient
	c, _ := client.NewHTTPClient(client.HTTPConfig{
		Addr:     cnf.Database.Host,
		Username: cnf.Database.DBUser,
		Password: cnf.Database.DbPwd,
	})
	defer c.Close()

	// Create a new point batch
	bp, _ := client.NewBatchPoints(client.BatchPointsConfig{
		Database: cnf.Database.DbName,
	})

	// Convert to number
	msGet := int64(get / time.Millisecond)
	msSave := int64(save / time.Millisecond)

	// Tags: Database name, environment name
	tags := map[string]string{cnf.Database.DbName: "timings"}
	fields := map[string]interface{}{
		"ds":   ds,
		"slen": slen,
		"get":  msGet,
		"save": msSave,
	}

	pt, _ := client.NewPoint(cnf.Database.DbName, tags, fields, start)

	bp.AddPoint(pt)
	c.Write(bp)

	// Close client resources
	c.Close()
}

//Save SLA data each minute
func influx1mPump(ds int) func(int) string {
	const thinkTime = 1000 * 60 // 60 seconds

	return func(ds int) string {
		// Wait a minute! :-)
		time.Sleep(time.Duration(thinkTime) * time.Millisecond)

		start := time.Now()

		// get 5 min of SLA data
		sla := getSLA(ds, 5, false)
		get := time.Since(start)

		influxSaveData(ds, sla)
		save := time.Since(start)

		//printSLA(sla)
		influxSaveTimings(ds, len(sla), start, get, save-get)

		return fmt.Sprintf("influx1mPump(%v) get:%v, save:%v", ds, get, save-get)
	}
}

//Save SLA data each hour, this is executed first every time on start. sleep = last
func influx1hPump(ds int) func(int) string {
	const thinkTime = 1000 * 60 * 60 //  60 minutes

	return func(ds int) string {

		start := time.Now()

		// get 240 minutes of SLA data
		sla := getSLA(ds, 240, false)
		get := time.Since(start)

		influxSaveData(ds, sla)
		save := time.Since(start)

		//printSLA(sla)

		t := fmt.Sprintf("influx1hPump(%v) get:%v, save:%v", ds, get, save-get)

		//fmt.Println(t)
		influxSaveTimings(ds, len(sla), start, get, save-get)

		// Wait an hour
		time.Sleep(time.Duration(thinkTime) * time.Millisecond)
		return t
	}
}

// Read configuration from file and return configuration srtruct
func getConfig() conf {
	var conf conf

	f, _ := ioutil.ReadFile("config.json")
	err := json.Unmarshal(f, &conf)

	if err != nil {
		fmt.Println(err)
	}

	return conf

}

func influxDataPump(ds int) {

	// callback functions
	saveEach1m := influx1mPump(ds)
	saveEach1h := influx1hPump(ds)

	pump := func(dataFunc func(ds int) string) string {
		for {
			dataFunc(ds)
			//fmt.Printf("%v %v\n", n, time.Now())
		}
	}

	go pump(saveEach1h)
	go pump(saveEach1m)

	select {}
}

/*
SLAVault =
Configure:  create InfluxDB database and configure one or more data sources in config.json
Get:        *one minute* Business Transaction(BT) *scorecard* data from one or several AppDynamics Contollers
Save:       one minute  *scorecard* into InfluxDB with possibility to calculate *SLA* -> calcSLA()
(Graph:)    can easily graph *SLA* from saved *scorecard* with your favorite tool Grafana/Graphite etc.
*/
func main() {
	// Get configuration
	c := getConfig()

	// For all data sources -> save SLA data periodically into InfluxDB
	// Thread the data pump for all datasources
	for ds := 0; ds < len(c.DataSources); ds++ {
		go influxDataPump(ds)
	}

	// Wait indefinitely - should not exit even when lost connection to data source or db
	select {}
}
