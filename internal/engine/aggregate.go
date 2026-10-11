package engine

import (
	"context"
	"fmt"

	"github.com/geoah/substrate/internal/substrate"
)

func (ds *dataset) Aggregate(ctx context.Context, q substrate.AggregateQuery) (*substrate.AggregateResult, error) {
	return nil, fmt.Errorf("%w: aggregate: not built yet", substrate.ErrValidation)
}
