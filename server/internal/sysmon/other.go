//go:build !linux && !darwin && !windows

package sysmon

import (
	"context"
	"runtime"
	"time"

	"github.com/nwverse/persona/server/internal/proto"
)

// ReadSystem knows only what Go knows on other systems.
func ReadSystem(ctx context.Context) proto.System {
	return proto.System{OS: runtime.GOOS, Distro: runtime.GOOS, Name: runtime.GOOS, Arch: runtime.GOARCH, Cores: runtime.NumCPU()}
}

type noCollector struct{}

func NewCollector(ctx context.Context) Collector { return noCollector{} }

func (noCollector) Sample(now time.Time) proto.MetricSample { return proto.MetricSample{T: now.Unix()} }
func (noCollector) Close()                                  {}

func loadAverageOS() *float64 { return nil }

// Volumes: a diagnosis does not list volumes on this system.
func Volumes() []proto.DiskVolume { return nil }
