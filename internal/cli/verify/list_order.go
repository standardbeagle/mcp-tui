package verify

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// listOrderProbe is the name of the tools/list ordering probe.
const listOrderProbe = "list-order"

// ProbeListOrder lists the server's tools twice and compares the order. The
// 2026-07-28 spec says servers SHOULD return tools in a deterministic order
// (the same ordering across requests when the set has not changed), so a
// changed order is a warning, not a failure. The spec asks this of
// tools/list only; prompts/list and resources/list carry no ordering rule.
//
// Both lists must reach the server. Within its ttlMs the SDK answers a
// repeated list from its per-session cache, which would compare the first
// answer with itself, so when the second list was served from the cache it
// is fetched again on a new session, which starts with an empty cache.
func ProbeListOrder(ctx context.Context, t *Target) ProbeResult {
	svc, failed := connectProbeService(ctx, listOrderProbe, t)
	if failed != nil {
		return *failed
	}
	defer disconnectProbeService(listOrderProbe, svc)

	first, err := listToolNames(ctx, svc)
	if err != nil {
		return listOrderListFailed(err)
	}
	second, err := listToolNames(ctx, svc)
	if err != nil {
		return listOrderListFailed(err)
	}
	if info := svc.ListCache("tools/list"); info != nil && info.CachedPages > 0 {
		fresh, failed := connectProbeService(ctx, listOrderProbe, t)
		if failed != nil {
			return *failed
		}
		defer disconnectProbeService(listOrderProbe, fresh)
		if second, err = listToolNames(ctx, fresh); err != nil {
			return listOrderListFailed(err)
		}
	}
	return classifyListOrder(first, second)
}

func listOrderListFailed(err error) ProbeResult {
	return ProbeResult{Name: listOrderProbe, Pass: false, Error: fmt.Sprintf("tools/list failed: %v", err),
		Fix: "make tools/list succeed before checking its order"}
}

// classifyListOrder is the pure decision of ProbeListOrder over two lists of
// tool names in the order the server returned them.
func classifyListOrder(first, second []string) ProbeResult {
	if slices.Equal(first, second) {
		return ProbeResult{Name: listOrderProbe, Pass: true}
	}
	if !slices.Equal(slices.Sorted(slices.Values(first)), slices.Sorted(slices.Values(second))) {
		return ProbeResult{
			Name:  listOrderProbe,
			Pass:  false,
			Error: "the tool set changed between the two tools/list requests, so their order cannot be compared",
			Fix:   "rerun while the server's tools are not changing",
		}
	}
	return ProbeResult{
		Name: listOrderProbe,
		Pass: true,
		Warn: true,
		Error: fmt.Sprintf("tools/list returned the same tools in a different order: [%s] then [%s]",
			strings.Join(first, ", "), strings.Join(second, ", ")),
		Fix: "return tools in a deterministic order, e.g. sorted by name; the 2026-07-28 spec says servers SHOULD, " +
			"so clients can cache the list and LLM prompt caches hit",
	}
}
