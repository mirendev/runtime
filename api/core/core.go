package compute

//go:generate go run ../../pkg/entity/cmd/schemagen -input schema.yml -output core_v1alpha/schema.gen.go -pkg core_v1alpha -export-contract core_v1alpha/cloud-export.gen.json -export-target cloud
