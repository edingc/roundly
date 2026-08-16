package account

import (
	"context"
	"fmt"
	"strings"

	"github.com/edingc/roundly/internal/course"
	"github.com/edingc/roundly/internal/database/sqlc"
	"github.com/edingc/roundly/internal/httpx"
	"github.com/edingc/roundly/internal/id"
	"github.com/edingc/roundly/internal/timex"
)

// maxReportedNames caps the name lists in the summary, so restoring five
// hundred courses does not return a wall of text.
const maxReportedNames = 50

// ImportCounts is what happened to one kind of thing.
type ImportCounts struct {
	Imported int `json:"imported"`
	Skipped  int `json:"skipped"`
	Failed   int `json:"failed"`
	// Names are truncated at maxReportedNames; Truncated says so rather than
	// letting the caller wonder why the list is shorter than the count.
	SkippedNames []string `json:"skipped_names"`
	FailedNames  []string `json:"failed_names"`
	Truncated    bool     `json:"truncated"`
}

func (c *ImportCounts) skip(name string) {
	c.Skipped++
	if len(c.SkippedNames) < maxReportedNames {
		c.SkippedNames = append(c.SkippedNames, name)
	} else {
		c.Truncated = true
	}
}

func (c *ImportCounts) fail(name string) {
	c.Failed++
	if len(c.FailedNames) < maxReportedNames {
		c.FailedNames = append(c.FailedNames, name)
	} else {
		c.Truncated = true
	}
}

// ProfileImportResult records which profile fields the merge filled in.
type ProfileImportResult struct {
	FieldsFilled  []string `json:"fields_filled"`
	FieldsSkipped []string `json:"fields_skipped"`
}

// ImportSummary is what a restore did. Everything is reported rather than
// assumed, because a merge that silently skipped half a file would look
// identical to one that worked.
type ImportSummary struct {
	FormatVersion int                 `json:"format_version"`
	Profile       ProfileImportResult `json:"profile"`
	Clubs         ImportCounts        `json:"clubs"`
	Courses       ImportCounts        `json:"courses"`
	Warnings      []string            `json:"warnings"`
}

// Import merges a backup into the caller's account, skipping anything that is
// already there.
//
// Non-destructive by design: nothing is overwritten and nothing is deleted, so
// importing the wrong file costs the user nothing, and importing the same file
// twice is a no-op. That property is also what makes a partial failure
// recoverable — the fix is to run the same file again.
func (s *Service) Import(ctx context.Context, userID string, exp *AccountExport) (*ImportSummary, error) {
	if err := course.CheckFormatVersion(exp.FormatVersion, accountExportFormatVersion, accountExportFormatVersion, "backup"); err != nil {
		return nil, err
	}
	if len(exp.Courses) > maxImportCourses {
		return nil, httpx.ValidationError(map[string]string{
			"courses": fmt.Sprintf("This file has %d courses; %d is the most that can be restored at once.", len(exp.Courses), maxImportCourses),
		})
	}
	if len(exp.Clubs) > maxImportClubs {
		return nil, httpx.ValidationError(map[string]string{
			"clubs": fmt.Sprintf("This file has %d clubs; %d is the most that can be restored at once.", len(exp.Clubs), maxImportClubs),
		})
	}

	summary := &ImportSummary{FormatVersion: exp.FormatVersion}
	if exp.Profile.AvatarJPEGBase64 == nil {
		summary.Warnings = append(summary.Warnings, "This backup contains no profile photo.")
	}

	if err := s.importProfile(ctx, userID, exp.Profile, summary); err != nil {
		return nil, err
	}
	if err := s.importClubs(ctx, userID, exp.Clubs, summary); err != nil {
		return nil, err
	}
	if err := s.importCourses(ctx, userID, exp.Courses, summary); err != nil {
		return nil, err
	}
	return summary, nil
}

// importProfile fills only the fields that are currently empty. A restore is
// not an overwrite: whatever the account says now was typed more recently than
// whatever is in the file.
func (s *Service) importProfile(ctx context.Context, userID string, p ProfileExport, summary *ImportSummary) error {
	row, err := s.db.Queries.GetUserByID(ctx, userID)
	if err != nil {
		return httpx.Internal(fmt.Errorf("load user: %w", err))
	}

	fill := func(field string, current *string, incoming *string) *string {
		if current != nil && strings.TrimSpace(*current) != "" {
			if incoming != nil && strings.TrimSpace(*incoming) != "" {
				summary.Profile.FieldsSkipped = append(summary.Profile.FieldsSkipped, field)
			}
			return current
		}
		if incoming == nil || strings.TrimSpace(*incoming) == "" {
			return current
		}
		summary.Profile.FieldsFilled = append(summary.Profile.FieldsFilled, field)
		return incoming
	}

	first := fill("first_name", row.FirstName, p.FirstName)
	last := fill("last_name", row.LastName, p.LastName)
	city := fill("location_city", row.LocationCity, p.LocationCity)
	region := fill("location_region", row.LocationRegion, p.LocationRegion)
	country := fill("location_country", row.LocationCountry, p.LocationCountry)

	// The email is never imported: it is the login identity, and a file must
	// not be able to move an account to a different address.
	displayName := row.DisplayName

	// The home course is matched by name among the user's own courses, since
	// IDs are not portable.
	homeCourse := row.HomeCourseID
	if homeCourse == nil && p.HomeCourseName != nil {
		if courses, err := s.db.Queries.ListCoursesByUploader(ctx, &userID); err == nil {
			for _, c := range courses {
				if strings.EqualFold(strings.TrimSpace(c.Name), strings.TrimSpace(*p.HomeCourseName)) {
					courseID := c.ID
					homeCourse = &courseID
					summary.Profile.FieldsFilled = append(summary.Profile.FieldsFilled, "home_course")
					break
				}
			}
		}
	}

	if err := s.db.Queries.UpdateUserProfile(ctx, sqlc.UpdateUserProfileParams{
		FirstName:       first,
		LastName:        last,
		DisplayName:     displayName,
		HomeCourseID:    homeCourse,
		LocationCity:    city,
		LocationRegion:  region,
		LocationCountry: country,
		UpdatedAt:       timex.Now(),
		ID:              userID,
	}); err != nil {
		return httpx.Internal(fmt.Errorf("import profile: %w", err))
	}
	return nil
}

// importClubs adds clubs the bag does not already hold.
//
// The identity key is (type, label), case- and space-insensitive. A bag has no
// natural unique key, and type-plus-label is what a person means by "the same
// club". The known cost: two genuinely different wedges both labelled
// "56 degree" collapse to one. For a merge that errs toward not duplicating,
// that is the right way to be wrong, and the summary reports the skip.
func (s *Service) importClubs(ctx context.Context, userID string, clubs []ClubExport, summary *ImportSummary) error {
	existing, err := s.db.Queries.ListClubsByUser(ctx, userID)
	if err != nil {
		return httpx.Internal(fmt.Errorf("list clubs: %w", err))
	}
	seen := make(map[string]bool, len(existing))
	for _, c := range existing {
		seen[clubKey(c.ClubType, c.Label)] = true
	}

	order := 0
	for _, c := range existing {
		if int(c.DisplayOrder) >= order {
			order = int(c.DisplayOrder) + 1
		}
	}

	// One transaction for the whole bag: it is bounded at maxImportClubs and is
	// far too small to be worth holding the single write connection per row.
	return s.db.InTx(func(q *sqlc.Queries) error {
		for _, c := range clubs {
			label := strings.TrimSpace(c.Label)
			clubType := strings.ToLower(strings.TrimSpace(c.ClubType))
			if label == "" || clubType == "" {
				summary.Clubs.fail(label)
				continue
			}
			key := clubKey(clubType, label)
			if seen[key] {
				summary.Clubs.skip(label)
				continue
			}

			active := int64(1)
			var retiredAt *string
			switch c.Status {
			case "retired":
				active, retiredAt = 0, c.RetiredAt
				if retiredAt == nil {
					now := timex.Now()
					retiredAt = &now
				}
			case "benched":
				active = 0
			}

			now := timex.Now()
			if err := q.CreateClub(ctx, sqlc.CreateClubParams{
				ID:                id.New(),
				UserID:            userID,
				ClubType:          clubType,
				Label:             label,
				Brand:             c.Brand,
				Model:             c.Model,
				Loft:              c.Loft,
				Shaft:             c.Shaft,
				Flex:              c.Flex,
				Notes:             c.Notes,
				ExpectedCarry:     int64Ptr(c.ExpectedCarry),
				AverageDispersion: int64Ptr(c.AverageDispersion),
				Active:            active,
				RetiredAt:         retiredAt,
				DisplayOrder:      int64(order),
				CreatedAt:         now,
				UpdatedAt:         now,
			}); err != nil {
				return fmt.Errorf("create club %q: %w", label, err)
			}
			seen[key] = true
			order++
			summary.Clubs.Imported++
		}
		return nil
	})
}

// importCourses adds courses the user does not already have one of by that name.
//
// Matching is on name among the importer's own courses, never on the ID in the
// file. The directory is shared, so an ID match could rewrite a course somebody
// else created — which is exactly what a restore must never do. A course
// someone else owns with the same name does not block anything; the user gets
// their own copy, which the schema allows.
func (s *Service) importCourses(ctx context.Context, userID string, courses []course.CourseExport, summary *ImportSummary) error {
	existing, err := s.db.Queries.ListCoursesByUploader(ctx, &userID)
	if err != nil {
		return httpx.Internal(fmt.Errorf("list courses: %w", err))
	}
	seen := make(map[string]bool, len(existing))
	for _, c := range existing {
		seen[strings.ToLower(strings.TrimSpace(c.Name))] = true
	}

	for _, c := range courses {
		name := strings.TrimSpace(c.Name)
		key := strings.ToLower(name)
		if name == "" {
			summary.Courses.fail("(unnamed)")
			continue
		}
		if seen[key] {
			summary.Courses.skip(name)
			continue
		}

		// Validated through the same rules as a single-course import, so the
		// two cannot drift apart.
		in, err := course.ValidateImport(c)
		if err != nil {
			summary.Courses.fail(name)
			continue
		}
		// Always create: the ID in the file may belong to another user's row.
		in.ID = ""

		// One transaction per course rather than one for the whole restore. The
		// pool has a single connection and the server times a request out at
		// thirty seconds; a single long transaction over hundreds of courses
		// would block every other request and then be rolled back anyway.
		if _, err := s.courses.Import(ctx, userID, in); err != nil {
			summary.Courses.fail(name)
			continue
		}
		seen[key] = true
		summary.Courses.Imported++
	}
	return nil
}

func clubKey(clubType, label string) string {
	return strings.ToLower(strings.TrimSpace(clubType)) + "\x00" + strings.ToLower(strings.TrimSpace(label))
}

func int64Ptr(v *int) *int64 {
	if v == nil {
		return nil
	}
	n := int64(*v)
	return &n
}
