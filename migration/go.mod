module github.com/philippgille/gokv/migration

go 1.23

require (
	github.com/philippgille/gokv v0.7.0
	github.com/philippgille/gokv/bbolt v0.7.0
	github.com/philippgille/gokv/syncmap v0.7.0
)

require (
	github.com/philippgille/gokv/encoding v0.7.0 // indirect
	github.com/philippgille/gokv/util v0.7.0 // indirect
	go.etcd.io/bbolt v1.4.2 // indirect
	golang.org/x/sys v0.29.0 // indirect
)

replace github.com/philippgille/gokv => ../

replace github.com/philippgille/gokv/bbolt => ../bbolt

replace github.com/philippgille/gokv/syncmap => ../syncmap
