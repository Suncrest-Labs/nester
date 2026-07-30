package timeseries

import (
	"sort"
	"time"

	"github.com/shopspring/decimal"
)

func Aggregate(points []Point, resolution Resolution) ([]RollupPoint, error) {
	if len(points) == 0 {
		return nil, nil
	}

	type keyedPoint struct {
		point       Point
		seriesKey   string
		bucketStart time.Time
	}

	keyed := make([]keyedPoint, 0, len(points))
	for _, point := range points {
		seriesKey, err := point.Series.Key()
		if err != nil {
			return nil, err
		}
		bucket, err := BucketStart(point.ObservedAt, resolution)
		if err != nil {
			return nil, err
		}
		keyed = append(keyed, keyedPoint{
			point:       point,
			seriesKey:   seriesKey,
			bucketStart: bucket,
		})
	}

	sort.Slice(keyed, func(i, j int) bool {
		if keyed[i].seriesKey != keyed[j].seriesKey {
			return keyed[i].seriesKey < keyed[j].seriesKey
		}
		if !keyed[i].bucketStart.Equal(keyed[j].bucketStart) {
			return keyed[i].bucketStart.Before(keyed[j].bucketStart)
		}
		return keyed[i].point.ObservedAt.Before(keyed[j].point.ObservedAt)
	})

	var out []RollupPoint
	var current *RollupPoint
	var sum decimal.Decimal
	var currentKey string
	for _, item := range keyed {
		groupKey := item.seriesKey + "|" + item.bucketStart.Format(time.RFC3339Nano)
		if current == nil || groupKey != currentKey {
			if current != nil {
				current.Average = sum.Div(decimal.NewFromInt(current.Count))
				out = append(out, *current)
			}
			currentKey = groupKey
			sum = decimal.Zero
			current = &RollupPoint{
				SeriesKey:   item.seriesKey,
				Metric:      item.point.Series.Metric,
				EntityType:  item.point.Series.EntityType,
				EntityID:    item.point.Series.EntityID,
				Resolution:  resolution,
				BucketStart: item.bucketStart,
				Open:        item.point.Value,
				High:        item.point.Value,
				Low:         item.point.Value,
				Close:       item.point.Value,
				Last:        item.point.Value,
			}
		}

		if item.point.Value.GreaterThan(current.High) {
			current.High = item.point.Value
		}
		if item.point.Value.LessThan(current.Low) {
			current.Low = item.point.Value
		}
		current.Close = item.point.Value
		current.Last = item.point.Value
		current.Count++
		sum = sum.Add(item.point.Value)
	}

	if current != nil {
		current.Average = sum.Div(decimal.NewFromInt(current.Count))
		out = append(out, *current)
	}
	return out, nil
}
