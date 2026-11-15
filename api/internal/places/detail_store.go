package places

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// Detail is the public read of one place: what a client needs after tapping a result.
//
// The read is five fixed statements — the place, its photos, its hours, its menus and its
// attributes — regardless of how much content the place has. Fetching an aggregate per row
// would be a query per aggregate.
func (s *PostgresStore) Detail(ctx context.Context, placeID string) (Detail, error) {
	detail, err := s.detailRow(ctx, placeID)
	if err != nil {
		return Detail{}, err
	}

	if detail.Photos, err = s.detailPhotos(ctx, placeID); err != nil {
		return Detail{}, err
	}
	if detail.Schedules, err = s.detailSchedules(ctx, placeID); err != nil {
		return Detail{}, err
	}
	if detail.Menus, err = s.detailMenus(ctx, placeID); err != nil {
		return Detail{}, err
	}
	if detail.Attributes, err = s.detailAttributes(ctx, placeID); err != nil {
		return Detail{}, err
	}
	return detail, nil
}

// detailRow reads the place itself.
//
// `openNow` is computed here, by the database, in the place's own timezone and with the
// overnight branch: the same rule the `openAt` discovery filter already uses. Computing it in
// Go would need a tz database the static alpine image does not carry, and a second copy of the
// rule drifts from the first.
//
// `has_hours` is read separately because a place with no hours is not closed — it is unknown,
// and the response must say `null` rather than `false`.
func (s *PostgresStore) detailRow(ctx context.Context, placeID string) (Detail, error) {
	const query = `
SELECT p.co_place::text, p.tx_title, p.tx_description,
       p.co_place_category::text, c.tx_name,
       p.tx_country, p.tx_region,
       p.nu_latitude::float8, p.nu_longitude::float8,
       p.tx_address, p.js_social_media, p.tx_timezone,
       EXISTS (SELECT 1 FROM core.tb_place_schedule h WHERE h.co_place = p.co_place) AS has_hours,
       EXISTS (
           SELECT 1
             FROM core.tb_place_schedule sc
            WHERE sc.co_place = p.co_place
              AND sc.nu_day_of_week = EXTRACT(ISODOW FROM (now() AT TIME ZONE p.tx_timezone))
              AND (
                (sc.dt_close_time > sc.dt_open_time
                  AND (now() AT TIME ZONE p.tx_timezone)::time
                      BETWEEN sc.dt_open_time AND sc.dt_close_time)
                OR
                (sc.dt_close_time <= sc.dt_open_time
                  AND ((now() AT TIME ZONE p.tx_timezone)::time >= sc.dt_open_time
                    OR (now() AT TIME ZONE p.tx_timezone)::time <= sc.dt_close_time))
              )) AS open_now
  FROM core.tb_place p
  JOIN core.tb_place_category c ON c.co_place_category = p.co_place_category
 WHERE p.co_place = $1::uuid
   AND p.dt_deleted_at IS NULL
   AND p.tx_status = 'ACTIVE'`

	var detail Detail
	var hasHours, openNow bool
	if err := s.pool.QueryRow(ctx, query, placeID).Scan(
		&detail.ID, &detail.Name, &detail.Description,
		&detail.Category.ID, &detail.Category.Name,
		&detail.Country, &detail.Region,
		&detail.Latitude, &detail.Longitude,
		&detail.Address, &detail.SocialMedia, &detail.Timezone,
		&hasHours, &openNow,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Detail{}, ErrNotFound
		}
		return Detail{}, wrapDetailError(err)
	}

	if hasHours {
		detail.OpenNow = &openNow
	}
	return detail, nil
}

// detailPhotos returns the cover first, then the gallery in its published order.
func (s *PostgresStore) detailPhotos(ctx context.Context, placeID string) ([]Photo, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT co_photo::text, tx_url, tx_type::text, nu_order
		  FROM core.tb_place_photo
		 WHERE co_place = $1::uuid
		   AND dt_deleted_at IS NULL AND tx_status = 'ACTIVE'
		 ORDER BY (tx_type = 'COVER') DESC, nu_order, dt_created_at`, placeID)
	if err != nil {
		return nil, wrapDetailError(err)
	}
	defer rows.Close()

	photos := make([]Photo, 0, 4)
	for rows.Next() {
		var photo Photo
		if err := rows.Scan(&photo.ID, &photo.URL, &photo.Type, &photo.Order); err != nil {
			return nil, wrapDetailError(err)
		}
		photos = append(photos, photo)
	}
	return photos, wrapDetailError(rows.Err())
}

// detailSchedules returns the weekly hours in ISO day order.
func (s *PostgresStore) detailSchedules(ctx context.Context, placeID string) ([]ScheduleDay, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT nu_day_of_week,
		       to_char(dt_open_time, 'HH24:MI'), to_char(dt_close_time, 'HH24:MI')
		  FROM core.tb_place_schedule
		 WHERE co_place = $1::uuid
		 ORDER BY nu_day_of_week, dt_open_time`, placeID)
	if err != nil {
		return nil, wrapDetailError(err)
	}
	defer rows.Close()

	days := make([]ScheduleDay, 0, 7)
	for rows.Next() {
		var day ScheduleDay
		if err := rows.Scan(&day.DayOfWeek, &day.Open, &day.Close); err != nil {
			return nil, wrapDetailError(err)
		}
		days = append(days, day)
	}
	return days, wrapDetailError(rows.Err())
}

// detailMenus returns the sections and their items in one statement. A section with no items
// is a real state: the LEFT JOIN yields a row with NULLs.
func (s *PostgresStore) detailMenus(ctx context.Context, placeID string) ([]Menu, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT m.co_menu::text, m.tx_name, m.tx_description,
		       i.tx_name, i.tx_description, i.nu_price::float8, c.tx_code
		  FROM core.tb_menu m
		  LEFT JOIN core.tb_menu_item i ON i.co_menu = m.co_menu
		                              AND i.dt_deleted_at IS NULL AND i.tx_status = 'ACTIVE'
		  LEFT JOIN core.tb_currency c ON c.co_currency = i.co_currency
		 WHERE m.co_place = $1::uuid
		   AND m.dt_deleted_at IS NULL AND m.tx_status = 'ACTIVE'
		 ORDER BY m.dt_created_at, m.co_menu, i.dt_created_at, i.co_menu_item`, placeID)
	if err != nil {
		return nil, wrapDetailError(err)
	}
	defer rows.Close()

	menus := make([]Menu, 0, 3)
	index := map[string]int{}
	for rows.Next() {
		var menuID, name string
		var description *string
		var itemName, itemDescription, currency *string
		var price *float64

		if err := rows.Scan(&menuID, &name, &description,
			&itemName, &itemDescription, &price, &currency); err != nil {
			return nil, wrapDetailError(err)
		}
		position, seen := index[menuID]
		if !seen {
			menus = append(menus, Menu{Name: name, Description: description, Items: []MenuItem{}})
			position = len(menus) - 1
			index[menuID] = position
		}
		if itemName != nil && price != nil && currency != nil {
			menus[position].Items = append(menus[position].Items, MenuItem{
				Name: *itemName, Description: itemDescription, Price: *price, Currency: *currency,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, wrapDetailError(err)
	}
	return menus, nil
}

// detailAttributes returns the governed attributes, by code.
func (s *PostgresStore) detailAttributes(ctx context.Context, placeID string) ([]AttributeRef, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT a.tx_code, a.tx_name
		  FROM core.tb_place_attribute_link l
		  JOIN core.tb_place_attribute a ON a.co_attribute = l.co_attribute
		 WHERE l.co_place = $1::uuid
		   AND a.dt_deleted_at IS NULL AND a.tx_status = 'ACTIVE'
		 ORDER BY a.tx_code`, placeID)
	if err != nil {
		return nil, wrapDetailError(err)
	}
	defer rows.Close()

	attributes := make([]AttributeRef, 0, 4)
	for rows.Next() {
		var attribute AttributeRef
		if err := rows.Scan(&attribute.Code, &attribute.Name); err != nil {
			return nil, wrapDetailError(err)
		}
		attributes = append(attributes, attribute)
	}
	return attributes, wrapDetailError(rows.Err())
}
