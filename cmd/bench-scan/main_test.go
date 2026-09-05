package main

import (
	"strings"
	"testing"
)

func TestCPUInfoModelRecordsNativeARMIdentifiers(t *testing.T) {
	const cpu = "processor: 0\nCPU implementer: 0x61\nCPU architecture: 8\nCPU variant: 0x0\nCPU part: 0x000\nCPU revision: 0\n"
	got, err := cpuInfoModel(cpu + "\n" + strings.Replace(cpu, "processor: 0", "processor: 1", 1))
	if err != nil {
		t.Fatal(err)
	}
	want := "ARM CPU IDs (/proc/cpuinfo): CPU implementer=0x61, CPU architecture=8, CPU variant=0x0, CPU part=0x000, CPU revision=0"
	if got != want {
		t.Fatalf("model=%q, want kernel IDs without inventing a board name", got)
	}
	if _, err := cpuInfoModel(strings.Replace(cpu, "CPU part: 0x000\n", "", 1)); err == nil {
		t.Fatal("accepted incomplete CPU identity")
	}
}

func TestCPUInfoModelPreservesNamedHardware(t *testing.T) {
	for _, tc := range []struct{ cpu, want string }{
		{"model name: Genuine processor model\nmodel: 142\n", "Genuine processor model"},
		{"Model: Raspberry Pi 4 Model B Rev 1.5\n", "Raspberry Pi 4 Model B Rev 1.5"},
	} {
		got, err := cpuInfoModel(tc.cpu)
		if err != nil || got != tc.want {
			t.Fatalf("model=%q error=%v, want %q", got, err, tc.want)
		}
	}
	if _, err := cpuInfoModel("processor: 0\nFeatures: fp asimd\n"); err == nil {
		t.Fatal("accepted missing model")
	}
}
