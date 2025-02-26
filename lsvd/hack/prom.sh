#!/bin/sh


prometheus --config.file=./hack/prometheus.yml --storage.tsdb.path=./tmp/prom
