package main

import (
	"time"

	runtimeconfig "github.com/bamanoz/tabula/internal/runtime/host/config"
	"github.com/bamanoz/tabula/internal/runtime/host/manifest"
	"github.com/bamanoz/tabula/internal/runtime/hostcmd"
)

func configuredRuntimeID(explicit string, now time.Time) (string, error) {
	return hostcmd.ConfiguredRuntimeID(explicit, now)
}

func workerKernelURL(configured string) string {
	return hostcmd.WorkerKernelURL(configured)
}

func prepareRuntimeEnvironment(tabulaHome string) {
	hostcmd.PrepareRuntimeEnvironment(tabulaHome)
}

func newManifestStore(cfg runtimeconfig.Config) (*manifest.Store, error) {
	return hostcmd.NewManifestStore(cfg)
}

func runtimePluginDirs(cfg runtimeconfig.Config) []string {
	return hostcmd.RuntimePluginDirs(cfg)
}
