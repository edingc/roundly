package account

import (
	"archive/zip"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
)

// writeCSVArchive streams the export as a ZIP of one CSV per table.
//
// Six files, not five: par and yardage live in hole_tee_details rather than on
// the hole, so a set without that file would silently drop every number on
// every scorecard.
//
// These are for reading in a spreadsheet, not for restoring — there is no
// format_version and no round-trip guarantee. JSON is the restorable format.
func writeCSVArchive(w io.Writer, exp *AccountExport) error {
	zw := zip.NewWriter(w)

	write := func(name string, header []string, rows [][]string) error {
		f, err := zw.Create(name)
		if err != nil {
			return fmt.Errorf("create %s: %w", name, err)
		}
		cw := csv.NewWriter(f)
		if err := cw.Write(header); err != nil {
			return fmt.Errorf("write %s header: %w", name, err)
		}
		for _, row := range rows {
			safe := make([]string, len(row))
			for i, cell := range row {
				safe[i] = safeCell(cell)
			}
			if err := cw.Write(safe); err != nil {
				return fmt.Errorf("write %s row: %w", name, err)
			}
		}
		cw.Flush()
		return cw.Error()
	}

	p := exp.Profile
	if err := write("profile.csv",
		[]string{"email", "display_name", "first_name", "last_name", "location_city",
			"location_region", "location_country", "distance_unit", "home_course_name", "created_at"},
		[][]string{{
			p.Email, p.DisplayName, deref(p.FirstName), deref(p.LastName), deref(p.LocationCity),
			deref(p.LocationRegion), deref(p.LocationCountry), p.DistanceUnit,
			deref(p.HomeCourseName), p.CreatedAt,
		}}); err != nil {
		return err
	}

	clubRows := make([][]string, 0, len(exp.Clubs))
	for _, c := range exp.Clubs {
		clubRows = append(clubRows, []string{
			c.ClubType, c.Label, deref(c.Brand), deref(c.Model), floatStr(c.Loft),
			deref(c.Shaft), deref(c.Flex), deref(c.Notes),
			intStr(c.ExpectedCarry), intStr(c.AverageDispersion),
			c.Status, deref(c.RetiredAt), strconv.Itoa(c.DisplayOrder),
		})
	}
	if err := write("clubs.csv",
		[]string{"club_type", "label", "brand", "model", "loft", "shaft", "flex", "notes",
			"expected_carry_yards", "average_dispersion_yards", "status", "retired_at", "display_order"},
		clubRows); err != nil {
		return err
	}

	// Courses and their children are joined on course_name and tee_name, the
	// same name-not-ID convention the JSON export uses.
	courseRows := make([][]string, 0, len(exp.Courses))
	teeRows := make([][]string, 0)
	holeRows := make([][]string, 0)
	detailRows := make([][]string, 0)

	for _, c := range exp.Courses {
		courseRows = append(courseRows, []string{
			c.Name, deref(c.Address), deref(c.Phone), deref(c.Website),
			deref(c.FacilityType), floatStr(c.Latitude), floatStr(c.Longitude),
		})
		for _, t := range c.Tees {
			teeRows = append(teeRows, []string{
				c.Name, t.Name, t.Color,
				floatStr(t.CourseRatingMen), intStr(t.SlopeRatingMen),
				floatStr(t.CourseRatingWomen), intStr(t.SlopeRatingWomen),
			})
		}
		for _, h := range c.Holes {
			holeRows = append(holeRows, []string{
				c.Name, strconv.Itoa(h.HoleNumber), intStr(h.HandicapIndex),
			})
			for _, d := range h.TeeDetails {
				detailRows = append(detailRows, []string{
					c.Name, strconv.Itoa(h.HoleNumber), d.TeeName,
					strconv.Itoa(d.Par), strconv.Itoa(d.Yardage),
				})
			}
		}
	}

	if err := write("courses.csv",
		[]string{"course_name", "address", "phone", "website", "facility_type", "latitude", "longitude"},
		courseRows); err != nil {
		return err
	}
	if err := write("tees.csv",
		[]string{"course_name", "tee_name", "color", "course_rating_men", "slope_rating_men",
			"course_rating_women", "slope_rating_women"},
		teeRows); err != nil {
		return err
	}
	if err := write("holes.csv",
		[]string{"course_name", "hole_number", "handicap_index"}, holeRows); err != nil {
		return err
	}
	if err := write("hole_tee_details.csv",
		[]string{"course_name", "hole_number", "tee_name", "par", "yardage_yards"},
		detailRows); err != nil {
		return err
	}

	return zw.Close()
}

// safeCell defuses a value a spreadsheet would execute.
//
// Excel and Sheets treat a leading =, +, -, @, tab, or carriage return as the
// start of a formula, so a club labelled `=HYPERLINK("http://evil","click")`
// becomes live content the moment the export is opened. Prefixing with an
// apostrophe forces it back to text.
func safeCell(s string) string {
	if s == "" {
		return s
	}
	switch s[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + s
	}
	return s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func intStr(v *int) string {
	if v == nil {
		return ""
	}
	return strconv.Itoa(*v)
}

func floatStr(v *float64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(*v, 'g', -1, 64)
}
