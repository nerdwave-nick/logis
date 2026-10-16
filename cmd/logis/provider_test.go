package main

import (
	"os"
	"reflect"
	"slices"
	"testing"
)

// Ported from Rust src/client/provider.rs the_launch_carries_argv_cwd_and_environment_as_bytes.
func TestTheLaunchCarriesArgvCwdAndEnvironmentAsBytes(t *testing.T) {
	l := providerLaunch([]string{"rite", "\xff"})
	if !reflect.DeepEqual(l.Argv, [][]byte{[]byte("rite"), []byte("\xff")}) {
		t.Fatalf("%q", l.Argv)
	}
	cwd, err := os.Getwd()
	if err != nil || string(l.Cwd) != cwd {
		t.Fatalf("%q, %v", l.Cwd, err)
	}
	want := []byte("PATH=" + os.Getenv("PATH"))
	if !slices.ContainsFunc(l.Env, func(kv []byte) bool { return string(kv) == string(want) }) {
		t.Fatalf("no %q in the environment", want)
	}
}
