package store

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strings"

	"github.com/Hikyo-Org/hikyo/internal/adapter"
)

func adapterOriginReview(message string) error {
	return fmt.Errorf("%w: %s", errors.Join(ErrConflict, adapter.ErrOperatorReview), message)
}

// Stored endpoint spelling is part of custody. Ambiguous legacy spellings
// require explicit keep-remote retirement, never an inferred ownership rewrite.
func requireCanonicalAdapterOrigin(provider, raw string) error {
	p, err := adapter.ParseProvider(provider)
	if err != nil {
		return err
	}
	canonical, err := adapter.CanonicalOrigin(p, raw)
	if err != nil || canonical != raw {
		return adapterOriginReview("stored provider origin requires canonical endpoint review and explicit keep-remote retirement")
	}
	return nil
}

// The private metadata door is intersected with this verified route and name.
// Pages are bounded, while an existing request/transaction/attempt deadline
// bounds total work. A partial scan can never authorize provider side effects.
func guardAdapterOriginCustody(ctx context.Context, q adapterOriginQueries, route adapterOriginRoute, targetID, surface, normalizedName string) error {
	if err := requireCanonicalAdapterOrigin(route.provider, route.origin); err != nil {
		return err
	}
	provider, err := adapter.ParseProvider(route.provider)
	if err != nil {
		return err
	}
	u, err := url.Parse(route.origin)
	if err != nil {
		return err
	}
	escape := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace
	authority := "https://" + u.Hostname()
	if strings.Contains(u.Hostname(), ":") {
		authority = "https://[" + u.Hostname() + "]"
	}
	dot := authority + "."
	_, ipError := netip.ParseAddr(u.Hostname())
	request := adapterOriginCandidateRequest{
		provider: route.provider, destinationKind: route.destinationKind, destinationScope: route.destinationScope,
		surface: surface, normalizedName: normalizedName, targetID: targetID, repositoryID: route.repositoryID, destinationID: route.destinationID,
		authority: authority, authorityRoot: escape(authority) + "/%", authorityPort: escape(authority) + ":%",
		authorityDot: dot, authorityDotRoot: escape(dot) + "/%", authorityDotPort: escape(dot) + ":%", ipv6: ipError == nil,
	}
	ctx, cancel := context.WithTimeout(ctx, adapter.AttemptTimeout)
	defer cancel()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		rows, err := q.heldCandidates(ctx, request)
		if err != nil {
			return err
		}
		for _, row := range rows {
			canonical, err := adapter.CanonicalOrigin(provider, row.origin)
			if err != nil {
				return adapterOriginReview("held provider origin requires operator review")
			}
			if row.targetID == targetID {
				if canonical != route.origin {
					return adapterOriginReview("historical held custody does not match the verified current route")
				}
			} else if canonical == route.origin {
				return adapterOriginReview("equivalent provider endpoint has foreign held custody; retire ambiguous claims explicitly before adoption")
			} else if provider == adapter.AWSSecretsManagerProvider {
				currentRegion, currentKnown := adapter.AWSOriginRegion(route.origin)
				otherRegion, otherKnown := adapter.AWSOriginRegion(canonical)
				if currentKnown && otherKnown && currentRegion == otherRegion {
					return adapterOriginReview("AWS endpoints share foreign account/region/name custody")
				}
			}
		}
		if len(rows) < 256 {
			return ctx.Err()
		}
		cursor := rows[len(rows)-1].id
		if cursor == request.cursor || cursor == "" {
			return adapterOriginReview("provider origin metadata cursor did not advance")
		}
		request.cursor = cursor
	}
}
