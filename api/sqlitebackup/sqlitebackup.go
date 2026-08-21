package sqlitebackup

//go:generate mkdir -p sqlitebackup_v1alpha
//go:generate go run ../../pkg/rpc/cmd/rpcgen -pkg sqlitebackup_v1alpha -input rpc.yml -output sqlitebackup_v1alpha/rpc.gen.go
