package account

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/edingc/roundly/internal/course"
	"github.com/edingc/roundly/internal/database/sqlc"
	"github.com/edingc/roundly/internal/httpx"
	"github.com/edingc/roundly/internal/timex"
)

// accountExportFormatVersion is the shape of a whole-account backup.
//
// Unlike the course export — which shipped as 1, 2, and 3 before anything ever
// read it back — this one is validated on import from the start. See
// course.CheckFormatVersion.
// Version 2 added rounds. A backup that carried the course directory and the
// golf bag but not the rounds played with them would have made "take your data"
// quietly untrue.
const accountExportFormatVersion = 2

// Caps on what a single restore may contain. A backup this large is either a
// mistake or an attack, and either way it would hold the one write connection
// well past the server's request timeout.
const (
	maxImportCourses = 500
	maxImportClubs   = 500
	// Rounds are the one collection that grows without bound over a lifetime,
	// so the cap is looser: a dedicated player records a couple of hundred a
	// year, and a restore should not refuse a decade of them.
	maxImportRounds = 5000
)

// AccountExport is everything the user owns, in a form that can be read by a
// human and fed back to POST /api/account/import.
//
// What is deliberately absent matters as much as what is here:
//
//   - password_hash, because an argon2id hash in a downloadable file is an
//     offline cracking target for whoever ends up with the file.
//   - refresh tokens and API keys, because they are live credentials; a restore
//     must never resurrect a key the user revoked.
//   - the OAuth provider subject, a stable Google identifier that would
//     correlate this person across every service they use.
//   - avatar_key, which is the unguessable part of the avatar URL — putting it
//     in a file people share would defeat the point of it.
//   - every row ID, which means nothing on another instance.
type AccountExport struct {
	FormatVersion int    `json:"format_version"`
	App           string `json:"app"`
	ExportedAt    string `json:"exported_at"`
	// Distances throughout this file are in yards, whatever the user reads them
	// in. Stated explicitly so a consumer never has to guess.
	DistanceUnitNote string `json:"distance_unit_note"`

	Profile ProfileExport         `json:"profile"`
	Clubs   []ClubExport          `json:"clubs"`
	Courses []course.CourseExport `json:"courses"`
	Rounds  []RoundExport         `json:"rounds"`
}

// RoundExport is a round without the identifiers that mean nothing elsewhere.
//
// The course and tee travel as the names the round snapshotted rather than as
// ids, matching how a course file references its own tees - and here it is more
// than a convention, because the snapshot is the authoritative record of what
// the course said that day. A round exported from an instance where the course
// has since been corrected still carries the pars it was played against.
type RoundExport struct {
	CourseName   string   `json:"course_name"`
	TeeName      string   `json:"tee_name"`
	CourseRating *float64 `json:"course_rating"`
	SlopeRating  *int     `json:"slope_rating"`
	PlayedOn     string   `json:"played_on"`
	Status       string   `json:"status"`
	EntryMode    string   `json:"entry_mode"`
	Holes        int      `json:"holes_intended"`
	Nine         *string  `json:"nine"`
	Notes        *string  `json:"notes"`

	HoleScores []RoundHoleExport `json:"hole_scores"`
}

// RoundHoleExport is one hole of one round. The tee club travels as its label
// for the same reason everything else does: a club id is meaningless on another
// instance, and the label is what a person reading the file wants anyway.
type RoundHoleExport struct {
	HoleNumber      int     `json:"hole_number"`
	Par             *int    `json:"par"`
	Yardage         *int    `json:"yardage"`
	StrokeIndex     *int    `json:"stroke_index"`
	Strokes         *int    `json:"strokes"`
	Putts           *int    `json:"putts"`
	TeeClubLabel    *string `json:"tee_club_label"`
	TeeAccuracy     *string `json:"tee_accuracy"`
	FirstPuttFeet   *int    `json:"first_putt_feet"`
	FairwayBunker   bool    `json:"fairway_bunker"`
	GreensideBunker bool    `json:"greenside_bunker"`
	Penalties       int     `json:"penalties"`
	PenaltyType     *string `json:"penalty_type"`
}

type ProfileExport struct {
	// Informational only: an import never changes the account's address.
	Email           string  `json:"email"`
	DisplayName     string  `json:"display_name"`
	FirstName       *string `json:"first_name"`
	LastName        *string `json:"last_name"`
	LocationCity    *string `json:"location_city"`
	LocationRegion  *string `json:"location_region"`
	LocationCountry *string `json:"location_country"`
	// Which published rating set applies. Restored like the rest of the
	// profile: filled only when the target account has not set one.
	Gender       *string `json:"gender"`
	DistanceUnit string  `json:"distance_unit"`
	// Matched by name on import: course IDs are not stable across instances,
	// the same reason a course file references its tees by name.
	HomeCourseName *string `json:"home_course_name"`
	// The avatar travels as base64 so a restore is actually complete. At a
	// 256x256 JPEG this is roughly 25KB, which is nothing beside the course
	// data, and a backup that quietly loses your photo is the kind of thing
	// people discover at the worst moment.
	AvatarJPEGBase64 *string  `json:"avatar_jpeg_base64"`
	LinkedProviders  []string `json:"linked_providers"`
	CreatedAt        string   `json:"created_at"`
}

// ClubExport is a club without the identifiers that mean nothing elsewhere.
// Distances are in yards, as stored.
type ClubExport struct {
	ClubType          string   `json:"club_type"`
	Label             string   `json:"label"`
	Brand             *string  `json:"brand"`
	Model             *string  `json:"model"`
	Loft              *float64 `json:"loft"`
	Shaft             *string  `json:"shaft"`
	Flex              *string  `json:"flex"`
	Notes             *string  `json:"notes"`
	ExpectedCarry     *int     `json:"expected_carry"`
	AverageDispersion *int     `json:"average_dispersion"`
	Status            string   `json:"status"`
	RetiredAt         *string  `json:"retired_at"`
	DisplayOrder      int      `json:"display_order"`
}

// Export gathers everything the user owns.
func (s *Service) Export(ctx context.Context, userID string) (*AccountExport, error) {
	row, err := s.db.Queries.GetUserByID(ctx, userID)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("load user: %w", err))
	}

	clubRows, err := s.db.Queries.ListClubsByUser(ctx, userID)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("list clubs: %w", err))
	}
	clubs := make([]ClubExport, 0, len(clubRows))
	for _, c := range clubRows {
		clubs = append(clubs, ClubExport{
			ClubType:          c.ClubType,
			Label:             c.Label,
			Brand:             c.Brand,
			Model:             c.Model,
			Loft:              c.Loft,
			Shaft:             c.Shaft,
			Flex:              c.Flex,
			Notes:             c.Notes,
			ExpectedCarry:     intPtr(c.ExpectedCarry),
			AverageDispersion: intPtr(c.AverageDispersion),
			Status:            clubStatus(c),
			RetiredAt:         c.RetiredAt,
			DisplayOrder:      int(c.DisplayOrder),
		})
	}

	courses, err := s.courses.ExportAll(ctx, userID)
	if err != nil {
		return nil, err
	}

	rounds, err := s.exportRounds(ctx, userID)
	if err != nil {
		return nil, err
	}

	accounts, err := s.db.Queries.ListOAuthAccountsByUser(ctx, userID)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("list oauth accounts: %w", err))
	}
	providers := make([]string, 0, len(accounts))
	for _, a := range accounts {
		// The provider name only. provider_subject is a stable cross-service
		// identifier and has no business in a file the user might share.
		providers = append(providers, a.Provider)
	}

	var avatar *string
	if av, err := s.db.Queries.GetAvatarByUser(ctx, userID); err == nil {
		encoded := base64.StdEncoding.EncodeToString(av.Image)
		avatar = &encoded
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, httpx.Internal(fmt.Errorf("load avatar: %w", err))
	}

	var homeCourseName *string
	if row.HomeCourseID != nil && *row.HomeCourseID != "" {
		if c, err := s.db.Queries.GetCourse(ctx, *row.HomeCourseID); err == nil {
			name := c.Name
			homeCourseName = &name
		}
	}

	return &AccountExport{
		FormatVersion:    accountExportFormatVersion,
		App:              "roundly",
		ExportedAt:       timex.Now(),
		DistanceUnitNote: "All distances in this file are in yards, regardless of the display preference.",
		Profile: ProfileExport{
			Email:            row.Email,
			DisplayName:      row.DisplayName,
			FirstName:        row.FirstName,
			LastName:         row.LastName,
			LocationCity:     row.LocationCity,
			LocationRegion:   row.LocationRegion,
			LocationCountry:  row.LocationCountry,
			Gender:           row.Gender,
			DistanceUnit:     row.DistanceUnit,
			HomeCourseName:   homeCourseName,
			AvatarJPEGBase64: avatar,
			LinkedProviders:  providers,
			CreatedAt:        row.CreatedAt,
		},
		Clubs:   clubs,
		Courses: courses,
		Rounds:  rounds,
	}, nil
}

// clubStatus mirrors the derivation in internal/club: status is computed from
// the two stored columns, never stored itself.
func clubStatus(c sqlc.Club) string {
	switch {
	case c.RetiredAt != nil:
		return "retired"
	case c.Active != 0:
		return "active"
	default:
		return "benched"
	}
}

func intPtr(v *int64) *int {
	if v == nil {
		return nil
	}
	n := int(*v)
	return &n
}

// exportRounds gathers every round with its holes.
//
// Two queries rather than one per round: a season is a couple of hundred rounds
// and this runs on the single write connection the whole server shares.
func (s *Service) exportRounds(ctx context.Context, userID string) ([]RoundExport, error) {
	rows, err := s.db.Queries.ListAllRounds(ctx, userID)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("list rounds: %w", err))
	}
	if len(rows) == 0 {
		return []RoundExport{}, nil
	}

	holeRows, err := s.db.Queries.ListAllRoundHolesByUser(ctx, userID)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("list round holes: %w", err))
	}
	byRound := make(map[string][]sqlc.RoundHole, len(rows))
	for _, h := range holeRows {
		byRound[h.RoundID] = append(byRound[h.RoundID], h)
	}

	// Club labels, resolved once. Retired clubs included: a round played with a
	// club since sold still has to name it.
	clubRows, err := s.db.Queries.ListClubsByUser(ctx, userID)
	if err != nil {
		return nil, httpx.Internal(fmt.Errorf("list clubs: %w", err))
	}
	labels := make(map[string]string, len(clubRows))
	for _, c := range clubRows {
		labels[c.ID] = c.Label
	}

	out := make([]RoundExport, 0, len(rows))
	for _, r := range rows {
		holes := make([]RoundHoleExport, 0, len(byRound[r.ID]))
		for _, h := range byRound[r.ID] {
			var clubLabel *string
			if h.TeeClubID != nil {
				if label, ok := labels[*h.TeeClubID]; ok {
					clubLabel = &label
				}
			}
			holes = append(holes, RoundHoleExport{
				HoleNumber:      int(h.HoleNumber),
				Par:             int64PtrToIntPtr(h.Par),
				Yardage:         int64PtrToIntPtr(h.Yardage),
				StrokeIndex:     int64PtrToIntPtr(h.StrokeIndex),
				Strokes:         int64PtrToIntPtr(h.Strokes),
				Putts:           int64PtrToIntPtr(h.Putts),
				TeeClubLabel:    clubLabel,
				TeeAccuracy:     h.TeeAccuracy,
				FirstPuttFeet:   int64PtrToIntPtr(h.FirstPuttFeet),
				FairwayBunker:   h.FairwayBunker != 0,
				GreensideBunker: h.GreensideBunker != 0,
				Penalties:       int(h.Penalties),
				PenaltyType:     h.PenaltyType,
			})
		}
		out = append(out, RoundExport{
			CourseName:   r.CourseName,
			TeeName:      r.TeeName,
			CourseRating: r.CourseRating,
			SlopeRating:  int64PtrToIntPtr(r.SlopeRating),
			PlayedOn:     r.PlayedOn,
			Status:       r.Status,
			EntryMode:    r.EntryMode,
			Holes:        int(r.HolesIntended),
			Nine:         r.Nine,
			Notes:        r.Notes,
			HoleScores:   holes,
		})
	}
	return out, nil
}

func int64PtrToIntPtr(v *int64) *int {
	if v == nil {
		return nil
	}
	converted := int(*v)
	return &converted
}
