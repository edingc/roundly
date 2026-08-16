package account

import (
	"context"
	"fmt"
	"strings"

	"github.com/edingc/roundly/internal/club"
	"github.com/edingc/roundly/internal/course"
	"github.com/edingc/roundly/internal/database/sqlc"
	"github.com/edingc/roundly/internal/httpx"
	"github.com/edingc/roundly/internal/id"
	"github.com/edingc/roundly/internal/round"
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
	Rounds        ImportCounts        `json:"rounds"`
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
	// Rounds last, because a round names its course by the name it snapshotted,
	// and importing the courses first means a restored round can point back at
	// a course that now exists on this instance.
	if err := s.importRounds(ctx, userID, exp.Rounds, summary); err != nil {
		return nil, err
	}
	return summary, nil
}

// importRounds adds rounds the account does not already have.
//
// The identity key is (played_on, course_name), which is what a person means by
// "the same round". The known cost: thirty-six holes at one course in one day
// collapse to one, and the summary reports the skip. For a merge that errs
// toward not duplicating a season every time a file is opened twice, that is
// the right way to be wrong.
//
// A round is restored with its snapshots intact rather than re-read off the
// course, because the snapshot is the record of what the course said that day.
// Recomputing it from this instance's copy of the course would silently restate
// the round - the exact failure the snapshot exists to prevent.
func (s *Service) importRounds(ctx context.Context, userID string, rounds []RoundExport, summary *ImportSummary) error {
	if len(rounds) > maxImportRounds {
		return httpx.ValidationError(map[string]string{
			"rounds": fmt.Sprintf("This file has %d rounds; %d is the most that can be restored at once.", len(rounds), maxImportRounds),
		})
	}

	existing, err := s.db.Queries.ListAllRounds(ctx, userID)
	if err != nil {
		return httpx.Internal(fmt.Errorf("list rounds: %w", err))
	}
	seen := make(map[string]bool, len(existing))
	for _, r := range existing {
		seen[roundKey(r.PlayedOn, r.CourseName)] = true
	}

	// Club labels the other way round, so a restored round can find the club it
	// was played with in this account's bag.
	clubRows, err := s.db.Queries.ListClubsByUser(ctx, userID)
	if err != nil {
		return httpx.Internal(fmt.Errorf("list clubs: %w", err))
	}
	clubIDs := make(map[string]string, len(clubRows))
	for _, c := range clubRows {
		clubIDs[strings.ToLower(strings.TrimSpace(c.Label))] = c.ID
	}

	// Courses on this instance, so a restored round can link back to one where
	// the name matches. A round whose course is not here keeps its snapshots and
	// simply has no link, which is the same state a removed course leaves.
	courseRows, err := s.db.Queries.ListCoursesByUploader(ctx, &userID)
	if err != nil {
		return httpx.Internal(fmt.Errorf("list courses: %w", err))
	}
	courseIDs := make(map[string]string, len(courseRows))
	for _, c := range courseRows {
		courseIDs[strings.ToLower(strings.TrimSpace(c.Name))] = c.ID
	}

	for _, r := range rounds {
		name := strings.TrimSpace(r.CourseName)
		label := fmt.Sprintf("%s (%s)", name, r.PlayedOn)
		if name == "" || r.PlayedOn == "" {
			summary.Rounds.fail(label)
			continue
		}
		key := roundKey(r.PlayedOn, name)
		if seen[key] {
			summary.Rounds.skip(label)
			continue
		}
		if err := s.restoreRound(ctx, userID, r, clubIDs, courseIDs); err != nil {
			summary.Rounds.fail(label)
			continue
		}
		seen[key] = true
		summary.Rounds.Imported++
	}
	return nil
}

func (s *Service) restoreRound(
	ctx context.Context,
	userID string,
	r RoundExport,
	clubIDs, courseIDs map[string]string,
) error {
	if _, ok := round.ParseDate(r.PlayedOn); !ok {
		return fmt.Errorf("invalid played_on %q", r.PlayedOn)
	}
	holes := r.Holes
	if holes != 9 && holes != 18 {
		holes = 18
	}
	status := r.Status
	if status != round.StatusComplete && status != round.StatusAbandoned {
		// An in-progress round is not worth restoring as one: the device that
		// was playing it is gone, and a stuck round in somebody's list is worse
		// than a finished one.
		status = round.StatusComplete
	}
	var courseID *string
	if id, ok := courseIDs[strings.ToLower(strings.TrimSpace(r.CourseName))]; ok {
		courseID = &id
	}

	roundID := id.New()
	now := timex.Now()
	return s.db.InTx(func(q *sqlc.Queries) error {
		if err := q.CreateRound(ctx, sqlc.CreateRoundParams{
			ID:           roundID,
			UserID:       userID,
			CourseID:     courseID,
			CourseName:   strings.TrimSpace(r.CourseName),
			TeeName:      strings.TrimSpace(r.TeeName),
			CourseRating: r.CourseRating,
			SlopeRating:  int64Ptr(r.SlopeRating),
			PlayedOn:     r.PlayedOn,
			// Created in progress and then moved, because the schema requires a
			// completed round to carry a completion time and CreateRound does
			// not set one. Two statements in one transaction rather than a
			// column that could drift from the status beside it.
			Status:        round.StatusInProgress,
			EntryMode:     round.EntryManual,
			HolesIntended: int64(holes),
			Nine:          r.Nine,
			Notes:         r.Notes,
			CreatedAt:     now,
			UpdatedAt:     now,
		}); err != nil {
			return fmt.Errorf("create round: %w", err)
		}
		for _, h := range r.HoleScores {
			if h.HoleNumber < 1 || h.HoleNumber > 18 {
				continue
			}
			var clubID *string
			if h.TeeClubLabel != nil {
				if id, ok := clubIDs[strings.ToLower(strings.TrimSpace(*h.TeeClubLabel))]; ok {
					clubID = &id
				}
			}
			if err := q.UpsertRoundHole(ctx, sqlc.UpsertRoundHoleParams{
				ID:              id.New(),
				RoundID:         roundID,
				HoleNumber:      int64(h.HoleNumber),
				Par:             int64Ptr(h.Par),
				Yardage:         int64Ptr(h.Yardage),
				StrokeIndex:     int64Ptr(h.StrokeIndex),
				Strokes:         int64Ptr(h.Strokes),
				Putts:           int64Ptr(h.Putts),
				TeeClubID:       clubID,
				TeeAccuracy:     h.TeeAccuracy,
				FirstPuttFeet:   int64Ptr(h.FirstPuttFeet),
				FairwayBunker:   boolToInt64(h.FairwayBunker),
				GreensideBunker: boolToInt64(h.GreensideBunker),
				Penalties:       int64(h.Penalties),
				PenaltyType:     h.PenaltyType,
			}); err != nil {
				return fmt.Errorf("restore hole %d: %w", h.HoleNumber, err)
			}
		}

		var completedAt *string
		if status == round.StatusComplete {
			completedAt = &now
		}
		return q.SetRoundStatus(ctx, sqlc.SetRoundStatusParams{
			Status:      status,
			CompletedAt: completedAt,
			UpdatedAt:   now,
			ID:          roundID,
			UserID:      userID,
		})
	})
}

func roundKey(playedOn, courseName string) string {
	return playedOn + "\x00" + strings.ToLower(strings.TrimSpace(courseName))
}

func boolToInt64(b bool) int64 {
	if b {
		return 1
	}
	return 0
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
	gender := fill("gender", row.Gender, p.Gender)

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

	// Gender is a preference with its own statement, so it is restored with its
	// own call. Same fill-only-if-empty rule as everything above.
	if gender != nil && (row.Gender == nil || *row.Gender == "") {
		if _, err := s.auth.SetGender(ctx, userID, gender); err != nil {
			return err
		}
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
			// Validated through the same rules as a club created through the
			// API, so a hand-edited file cannot store what the app itself would
			// refuse — the same argument importCourses makes below.
			in, err := club.ValidateImport(club.ImportClub{
				Type:              c.ClubType,
				Label:             c.Label,
				Brand:             c.Brand,
				Model:             c.Model,
				Loft:              c.Loft,
				Shaft:             c.Shaft,
				Flex:              c.Flex,
				Notes:             c.Notes,
				ExpectedCarry:     c.ExpectedCarry,
				AverageDispersion: c.AverageDispersion,
			})
			if err != nil {
				summary.Clubs.fail(labelOrPlaceholder(label))
				continue
			}
			clubType := in.Type
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
				Label:             strings.TrimSpace(in.Label),
				Brand:             in.Brand,
				Model:             in.Model,
				Loft:              in.Loft,
				Shaft:             in.Shaft,
				Flex:              in.Flex,
				Notes:             in.Notes,
				ExpectedCarry:     int64Ptr(in.ExpectedCarry),
				AverageDispersion: int64Ptr(in.AverageDispersion),
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

// labelOrPlaceholder keeps a nameless club from being reported as a blank line
// in the import summary.
func labelOrPlaceholder(label string) string {
	if label == "" {
		return "(unnamed)"
	}
	return label
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
