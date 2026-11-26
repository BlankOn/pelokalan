package postgres

import (
	"context"

	"pelokalan/internal/models"
)

// Translation Project team pages (tp_domains, migration 047)

func (r *glossaryRepository) SetGroupTP(ctx context.Context, groupID int64, team string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE module_groups SET tp_team = $2 WHERE id = $1`, groupID, team)
	return err
}

func (r *glossaryRepository) TPDomains(ctx context.Context, team string, withData bool) ([]*models.TPDomain, error) {
	data := `NULL::bytea`
	if withData {
		data = `data`
	}
	rows, err := r.db.QueryContext(ctx, `SELECT team, domain, version, file_url, template, translated, total, updated,
		translator, restricted, fingerprint, `+data+` FROM tp_domains WHERE team = $1 ORDER BY domain`, team)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*models.TPDomain
	for rows.Next() {
		d := &models.TPDomain{}
		if err := rows.Scan(&d.Team, &d.Domain, &d.Version, &d.FileURL, &d.Template, &d.Translated, &d.Total, &d.Updated,
			&d.Translator, &d.Restricted, &d.Fingerprint, &d.Data); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (r *glossaryRepository) SaveTPDomains(ctx context.Context, team string, domains []*models.TPDomain) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	names := make([]string, 0, len(domains))
	for _, d := range domains {
		names = append(names, d.Domain)
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO tp_domains (team, domain, version, file_url, template, translated, total, updated, translator,
			                        restricted, fingerprint, data, checked_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, now())
			ON CONFLICT (team, domain) DO UPDATE SET version = EXCLUDED.version, file_url = EXCLUDED.file_url,
			    template = EXCLUDED.template, translated = EXCLUDED.translated, total = EXCLUDED.total,
			    updated = EXCLUDED.updated, translator = EXCLUDED.translator, restricted = EXCLUDED.restricted,
			    fingerprint = EXCLUDED.fingerprint, data = COALESCE(EXCLUDED.data, tp_domains.data), checked_at = now()`,
			team, d.Domain, d.Version, d.FileURL, d.Template, d.Translated, d.Total, d.Updated, d.Translator,
			d.Restricted, d.Fingerprint, d.Data); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM tp_domains WHERE team = $1 AND NOT (domain = ANY($2))`, team, names); err != nil {
		return err
	}
	return tx.Commit()
}
