package setup

import (
	"context"
	"testing"
)

func TestRun_RequiresToken(t *testing.T) {
	_, _, err := Run(context.Background(), Options{ConfigPath: "/tmp/spoolr-test.json"})
	if err == nil {
		t.Fatal("expected an error when the pairing token is empty")
	}
}

func TestRun_RequiresConfigPath(t *testing.T) {
	_, _, err := Run(context.Background(), Options{Token: "spk_test"})
	if err == nil {
		t.Fatal("expected an error when the config path is empty")
	}
}
