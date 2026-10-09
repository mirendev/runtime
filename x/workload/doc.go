// Package workload hosts Session workloads inside a Miren sandbox.
//
// Host manages the metadata protocol for dedicated and shared Session hosts:
// assignment long polls, cancellation, cleanup acknowledgments, aggregate work
// activity, and advance shutdown notices. Applications supply a StartFunc that
// returns a StopFunc and call Host.Begin before accepting each piece of work.
// Client exposes the same protocol for applications that manage their own loops.
//
// This package uses only the standard library and does not import runtime
// internals or agent libraries. Like other packages under x/, its API may change
// between versions; pin the version you import.
package workload
