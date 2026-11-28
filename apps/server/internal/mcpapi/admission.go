package mcpapi

import (
	"context"
	"math"
	"time"

	"danny.vn/trachuan/apps/server/internal/apierror"
	"danny.vn/trachuan/apps/server/internal/credential"
	"danny.vn/trachuan/apps/server/internal/frameworkaccess"
	"danny.vn/trachuan/apps/server/internal/limits"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type admissionKey struct{}

type admission struct {
	method  string
	allowed bool
	retry   time.Duration
}

type visibilityKey struct{}
type discoveryLimitKey struct{}

type discoveryLimit struct {
	limited bool
	retry   time.Duration
}

func frameworkVisible(ctx context.Context) bool {
	visible, ok := ctx.Value(visibilityKey{}).(bool)
	return !ok || visible
}

func frameworkMiddleware(cfg Config, offTools map[string]*mcp.Tool) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method != "tools/list" && method != "tools/call" {
				return next(ctx, method, req)
			}
			var list *mcp.ListToolsResult
			if method == "tools/list" {
				result, err := next(ctx, method, req)
				if err != nil {
					return nil, err
				}
				var ok bool
				list, ok = result.(*mcp.ListToolsResult)
				if !ok {
					return result, nil
				}
			}
			principal, ok := credential.FromContext(ctx)
			if !ok || principal.AccountID <= 0 || principal.Scope != "mcp:read" {
				return nil, &jsonrpc.Error{Code: -32603, Message: "unavailable"}
			}
			limiter := cfg.Limiter
			class := limits.Read
			var name string
			if method == "tools/list" {
				limiter = cfg.DiscoveryLimiter
			} else {
				call, ok := req.(*mcp.CallToolRequest)
				if !ok || call.Params == nil {
					return nil, &jsonrpc.Error{Code: -32602, Message: "invalid request"}
				}
				name = call.Params.Name
				if _, registered := offTools[name]; !registered && name != "mappings" {
					return next(ctx, method, req)
				}
				if name == "search" {
					class = limits.Search
				}
			}
			if limiter == nil {
				if method == "tools/call" {
					return toolFailure("unavailable", apierror.Message("unavailable")), nil
				}
				return nil, &jsonrpc.Error{Code: -32603, Message: "unavailable"}
			}
			allowed, retry := limiter.Allow(principal.AccountID, class)
			if !allowed {
				if method == "tools/call" {
					seconds := max(int64(1), int64(math.Ceil(retry.Seconds())))
					return toolFailure("rate_limited", apierror.RetryMessage(int(seconds))), nil
				}
				if state, ok := ctx.Value(discoveryLimitKey{}).(*discoveryLimit); ok {
					state.limited, state.retry = true, retry
				}
				return nil, &jsonrpc.Error{Code: -32000, Message: "rate limited"}
			}
			ctx = frameworkaccess.RequestContext(ctx)
			visible := cfg.Frameworks
			if cfg.FrameworkEntitled != nil {
				entitled := cfg.FrameworkEntitled(ctx, principal.AccountID)
				visible = visible && entitled
			}
			ctx = context.WithValue(ctx, visibilityKey{}, visible)
			if method == "tools/list" {
				if !visible {
					filtered := make([]*mcp.Tool, 0, len(list.Tools))
					for _, tool := range list.Tools {
						if off, ok := offTools[tool.Name]; ok {
							filtered = append(filtered, off)
						}
					}
					list.Tools = filtered
				}
				return list, nil
			}
			if name == "mappings" && !visible {
				return nil, &jsonrpc.Error{Code: -32602, Message: "unknown tool"}
			}
			ctx = context.WithValue(ctx, admissionKey{}, admission{method: "tools/call", allowed: true})
			return next(ctx, method, req)
		}
	}
}
