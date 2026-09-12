package cli

import (
	"context"
	"strings"
	"testing"
)

func TestParameterDeleteRejectsInvalidNameAndPattern(t *testing.T) {
	for _, args := range [][]string{
		{"delete", "--name", "NAME", "--pattern", ".*"},
		{"delete", "--name", "NAME", "--pattern="},
		{"delete", "--name", "bad"},
	} {
		ios, _ := tokenIO(map[string]string{"HIKYO_STATE_DIR": t.TempDir()})
		err := runEnvParam(context.Background(), ios, args)
		if err == nil || (!strings.Contains(err.Error(), "pattern") && !strings.Contains(err.Error(), "name")) {
			t.Fatalf("%v: %v", args, err)
		}
	}
}

func TestParameterFlagRefusesDuplicateAndMalformedInputs(t *testing.T) {
	values := map[string]string{}
	parse := parameterFlag(values)
	if err := parse("PR_NUMBER=123"); err != nil {
		t.Fatal(err)
	}
	if err := parse("PR_NUMBER=124"); err == nil {
		t.Fatal("duplicate silently replaced first parameter")
	}
	if values["PR_NUMBER"] != "123" {
		t.Fatal("duplicate mutated accepted value")
	}
	if err := parse("OTHER"); err == nil {
		t.Fatal("missing equals accepted")
	}
	if err := parse("bad=value"); err == nil {
		t.Fatal("invalid name accepted")
	}
}
