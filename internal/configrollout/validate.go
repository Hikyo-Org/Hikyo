package configrollout

import (
	"net"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// The request Secret is a protocol boundary too. A recomputed digest must not
// turn its typed deltas into arbitrary environment or argument authority.
func (k *Kubernetes) validPlan(p planData) bool {
	if !validIntent(p.Intent) || !validDigest(p.BeforeMetadataDigest) || p.TargetDigest != k.targetDigest || p.Replicas < 1 || len(p.Changes) == 0 && p.Bootstrap == nil || len(p.Changes) > 32 || !k.validBootstrapDelta(p) {
		return false
	}
	after := cloneData(p.ConfigBefore)
	for key := range after {
		if !allowed(Variable(key)) {
			return false
		}
	}
	listenerValues := map[string]string{}
	flagValues := map[string]string{}
	ports := map[string]int32{}
	for i, change := range p.Changes {
		if i > 0 && p.Changes[i-1].Variable >= change.Variable {
			return false
		}
		value, err := k.resolve(change, p.Replicas)
		if err != nil {
			return false
		}
		after[string(change.Variable)] = []byte(value)
		e := p.Delta.Environment[i]
		want := corev1.EnvVar{Name: string(change.Variable), ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: k.target.ConfigSecret}, Key: string(change.Variable)}}}
		if e.Name != string(change.Variable) || digest(e.After) != digest(want) || e.Before != nil && e.Before.Name != e.Name {
			return false
		}
		if flag := variableFlag(change.Variable); flag != "" {
			flagValues[flag] = value
		}
		if change.Variable == Listen || change.Variable == OperationalListen {
			flag := "--listen"
			if change.Variable == OperationalListen {
				flag = "--operational-listen"
			}
			listenerValues[flag] = value
			_, port, _ := net.SplitHostPort(value)
			number, _ := strconv.ParseInt(port, 10, 32)
			name := "http"
			if change.Variable == OperationalListen {
				name = "ops"
			}
			ports[name] = int32(number)
		}
	}
	if !sameData(after, p.ConfigAfter) || len(p.Delta.Ports) != len(listenerValues) || len(p.Delta.Arguments) > len(flagValues) {
		return false
	}
	seenArgs := map[int]bool{}
	seenFlags := map[string]bool{}
	for _, arg := range p.Delta.Arguments {
		if arg.Index < 0 || seenArgs[arg.Index] || seenFlags[arg.Flag] {
			return false
		}
		seenArgs[arg.Index] = true
		seenFlags[arg.Flag] = true
		permitted := false
		for flag, value := range flagValues {
			if arg.Flag != flag {
				continue
			}
			if arg.After == flag+"="+value && strings.HasPrefix(arg.Before, flag+"=") {
				permitted = true
			}
			if arg.After == value {
				permitted = arg.Before != "" && !strings.HasPrefix(arg.Before, "-") && !strings.ContainsAny(arg.Before, "\x00\r\n")
			}
		}
		if !permitted {
			return false
		}
	}
	seenPorts := map[int]bool{}
	seenNames := map[string]bool{}
	for _, port := range p.Delta.Ports {
		if port.Index < 0 || seenPorts[port.Index] || seenNames[port.Name] || ports[port.Name] != port.After || port.Before < 1 || port.Before > 65535 {
			return false
		}
		seenPorts[port.Index] = true
		seenNames[port.Name] = true
	}
	return true
}
