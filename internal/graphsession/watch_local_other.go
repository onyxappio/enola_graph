//go:build !linux && !darwin

package graphsession

import "fmt"

func requireLocalWatch(path string) error {
	return fmt.Errorf("resident filesystem watch coverage is currently audited only on Darwin and Linux; use strict reconciliation: %s", path)
}
