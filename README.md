# sla-vault
Retain high resolution perf data from appdynamics
Configure:  create InfluxDB database and configure one or more data sources in config.json
Get:        *one minute* Business Transaction(BT) *scorecard* data from one or several data sources
Save:       one minute  *scorecard* into InfluxDB with possibility to calculate *SLA* -> calcSLA()
(Graph:)    can easily graph *SLA* from saved *scorecard* with your favorite tool Grafana/Graphite etc.

Example config:

{
    "database": {
        "db_host": "http://localhost:8086",
        "db_name": "SLA",
        "db_user": "xxx",
        "db_pwd": "xxx"
    },
    "data_sources": [
         {
            "unique_name": "perf",
            "host": "http://AppDController1:8090/controller/rest/applications/myApplication1",
            "metric_path": "Business Transaction Performance|Business Transaction Groups|SLA|*",
            "marest_user": "rest@customer1",
            "rest_pwd": "xxx"

        },
        {
            "unique_name": "BT_test_env",
            "host": "http://myAppDController2:8090/controller/rest/applications/myApplication2",
            "metric_path": "Business Transaction Performance|Business Transactions|*|*|*",
            "rest_user": "rest@customer1",
            "rest_pwd": "xxx"

        }
    ]
}