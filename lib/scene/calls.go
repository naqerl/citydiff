package scene

import (
	"reflect"

	"citydiff/lib"
)

// alignCalls diffs two call lists in source order.
// A resolved call matches on the declaration it names. An unresolved call
// matches on the callee text. Repeated calls stay repeated: the alignment
// is a sequence, not a set.
func alignCalls(left, right []lib.Call, change string) []CallStep {
	switch change {
	case added:
		return markCalls(right, added)
	case removed:
		return markCalls(left, removed)
	case same:
		return markCalls(right, same)
	default:
		if reflect.DeepEqual(left, right) {
			return markCalls(right, same)
		}
		return diffCalls(left, right)
	}
}

func markCalls(calls []lib.Call, change string) []CallStep {
	if len(calls) == 0 {
		return nil
	}
	out := make([]CallStep, len(calls))
	for i, call := range calls {
		out[i] = callStep(call, change)
	}
	return out
}

func callStep(call lib.Call, change string) CallStep {
	step := CallStep{Change: change, Expr: call.Expr}
	if call.Ref != nil {
		step.Path = call.Ref.Path
		step.Name = call.Ref.Name
		step.Recv = call.Ref.Recv
		step.Resolved = true
	}
	return step
}

// callKey is the identity of one step in a call sequence.
func callKey(call lib.Call) string {
	if call.Ref != nil {
		return "r\x00" + call.Ref.Path + "\x00" + call.Ref.Recv + "\x00" + call.Ref.Name
	}
	return "e\x00" + call.Expr
}

// diffCalls is a longest-common-subsequence alignment.
// On a tie it consumes the left call, so a deleted duplicate stays next to
// the copy that survived.
func diffCalls(left, right []lib.Call) []CallStep {
	n, m := len(left), len(right)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if callKey(left[i]) == callKey(right[j]) {
				dp[i][j] = dp[i+1][j+1] + 1
				continue
			}
			if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
				continue
			}
			dp[i][j] = dp[i][j+1]
		}
	}
	out := make([]CallStep, 0, n+m)
	i, j := 0, 0
	for i < n && j < m {
		if callKey(left[i]) == callKey(right[j]) {
			out = append(out, callStep(right[j], same))
			i++
			j++
			continue
		}
		if dp[i+1][j] >= dp[i][j+1] {
			out = append(out, callStep(left[i], removed))
			i++
			continue
		}
		out = append(out, callStep(right[j], added))
		j++
	}
	for ; i < n; i++ {
		out = append(out, callStep(left[i], removed))
	}
	for ; j < m; j++ {
		out = append(out, callStep(right[j], added))
	}
	return out
}
