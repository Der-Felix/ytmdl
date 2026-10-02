package repository

import (
	"context"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"ytdm/backend/internal/apperr"
)

// NormalizeGenres bounds shared catalog metadata and removes case-insensitive duplicates.
func NormalizeGenres(values []string) ([]string, error) {
	if len(values) > 20 {
		return nil, apperr.New(apperr.CodeInvalidRequest, "Höchstens 20 Genres pro Künstler sind erlaubt.")
	}
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if utf8.RuneCountInString(value) > 80 || strings.ContainsFunc(value, unicode.IsControl) {
			return nil, apperr.New(apperr.CodeInvalidRequest, "Genres dürfen höchstens 80 Zeichen und keine Steuerzeichen enthalten.")
		}
		key := strings.ToLower(value)
		if !seen[key] {
			seen[key] = true
			out = append(out, value)
		}
	}
	return out, nil
}

func (c *Catalog) SetArtistGenres(ctx context.Context, id string, genres []string) ([]string, error) {
	normalized, err := NormalizeGenres(genres)
	if err != nil {
		return nil, err
	}
	result, err := c.db.ExecContext(ctx, `UPDATE artists SET genres_json = $1::jsonb, genres_manual = true, updated_at = now() WHERE id = $2`, encodeStrings(normalized), id)
	if err != nil {
		return nil, wrapDB("set artist genres", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return nil, wrapDB("count updated artists", err)
	}
	if count == 0 {
		return nil, apperr.New(apperr.CodeArtistNotFound, "Dieser Künstler ist nicht in der Bibliothek.")
	}
	return normalized, nil
}

func (c *Catalog) ListGenres(ctx context.Context) ([]string, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT DISTINCT ON (LOWER(g COLLATE "pg_c_utf8")) g FROM artists, jsonb_array_elements_text(genres_json) g ORDER BY LOWER(g COLLATE "pg_c_utf8"), g`)
	if err != nil {
		return nil, wrapDB("list genres", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var g string
		if err := rows.Scan(&g); err != nil {
			return nil, wrapDB("scan genre", err)
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func appendGenreFilter(genre string, missing bool, owner string, clauses *[]string, args *[]any, index *int) error {
	genre = strings.TrimSpace(genre)
	if genre != "" && missing {
		return apperr.New(apperr.CodeInvalidRequest, "Genre und Ohne Genre können nicht gleichzeitig gewählt werden.")
	}
	if genre != "" {
		if _, err := NormalizeGenres([]string{genre}); err != nil {
			return err
		}
		*clauses = append(*clauses, fmt.Sprintf(`EXISTS (SELECT 1 FROM artists ga, jsonb_array_elements_text(ga.genres_json) g WHERE ga.id = %s AND LOWER(g COLLATE "pg_c_utf8") = LOWER($%d COLLATE "pg_c_utf8"))`, owner, *index))
		*args = append(*args, genre)
		*index++
	} else if missing {
		*clauses = append(*clauses, fmt.Sprintf(`NOT EXISTS (SELECT 1 FROM artists ga WHERE ga.id = %s AND ga.genres_json != '[]'::jsonb)`, owner))
	}
	return nil
}

// ArtworkFilePaths returns a bounded set of catalog-owned files, never a request path.
func (c *Catalog) ArtworkFilePaths(ctx context.Context, kind, id string) ([]string, error) {
	owner := map[string]string{"artists": "t.artist_id", "releases": "t.release_id", "tracks": "t.id"}[kind]
	if owner == "" {
		return nil, apperr.New(apperr.CodeInvalidRequest, "Unknown artwork kind.")
	}
	rows, err := c.db.QueryContext(ctx, `SELECT MIN(f.path) FROM files f JOIN tracks t ON t.id = f.track_id WHERE `+owner+` = $1 AND f.path != '' GROUP BY regexp_replace(f.path, '/[^/]+$', '') ORDER BY MIN(f.path) LIMIT 64`, id)
	if err != nil {
		return nil, wrapDB("find local artwork", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, wrapDB("scan artwork file", err)
		}
		out = append(out, path)
	}
	return out, rows.Err()
}
