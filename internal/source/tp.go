package source

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"pelokalan/internal/models"
	"pelokalan/internal/platform/apperr"
	"pelokalan/internal/platform/gz"
	"pelokalan/pkg/glossary"
	"pelokalan/pkg/po"
	"pelokalan/pkg/tpsite"
)

// Module groups synced from a Translation Project team page (pkg/tpsite). The site has no API:
// FetchGroup reads the team page (ETag/Last-Modified as for any link), downloads the team's file of
// every domain whose row changed (others come from tp_domains, shared by all projects), and imports
// them as one archive, so the glossary, project files, "Ambil"/"Perbarui" and the upstream landings
// work as for the other sources. Translations go back by email from the translator (the robot checks
// the sender), so Pelokalan only names the file and fills the email (TPSubmit).

// tpPause is the wait between two file downloads from the site (be gentle: ~150 files on the first sync)
var tpPause = 300 * time.Millisecond

// fetchTP downloads a team page and the files of its domains as one .tar.gz
func (s *Service) fetchTP(ctx context.Context, v *models.ModuleGroup, etag, lastMod string) (*glossary.FetchResult, error) {
	page, err := s.fetcher.Fetch(ctx, v.SyncURL, etag, lastMod)
	if err != nil {
		return nil, err
	}
	rows, err := tpsite.ParseTeamPage(page.Data)
	if err != nil {
		return nil, apperr.New("err.tp_page")
	}
	team := v.TPTeam
	stored, err := s.repo.TPDomains(ctx, team, true)
	if err != nil {
		return nil, err
	}
	old := map[string]*models.TPDomain{}
	for _, d := range stored {
		old[d.Domain] = d
	}

	var archive bytes.Buffer
	zw := gzip.NewWriter(&archive)
	tw := tar.NewWriter(zw)
	var recs []*models.TPDomain
	downloads, failed, files := 0, 0, 0
	for _, d := range rows {
		rec := &models.TPDomain{Team: team, Domain: d.Name, Version: d.Version, FileURL: d.FileURL, Template: d.Template,
			Translated: d.Translated, Total: d.Total, Translator: d.Translator, Restricted: d.Restricted,
			Fingerprint: d.Fingerprint()}
		if !d.Updated.IsZero() {
			t := d.Updated
			rec.Updated = &t
		}
		recs = append(recs, rec)
		var data []byte
		prev := old[d.Name]
		if prev != nil && prev.Fingerprint == rec.Fingerprint && len(prev.Data) > 0 {
			data, _ = gz.Decompress(prev.Data)
		}
		if data == nil {
			if downloads > 0 {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(tpPause):
				}
			}
			downloads++
			data, err = s.tpFile(ctx, d, team)
			if err != nil {
				failed++
				log.Printf("translation project: %s (%s): %v", d.Name, team, err)
				if prev != nil && len(prev.Data) > 0 { // keep the last good copy; tried again next time
					rec.Fingerprint = prev.Fingerprint
					data, _ = gz.Decompress(prev.Data)
				} else {
					rec.Fingerprint = ""
				}
			} else {
				rec.Data = gz.Compress(data)
			}
		}
		if data == nil {
			continue
		}
		if err := tw.WriteHeader(&tar.Header{Name: tpsite.FileName(d.Name, team), Mode: 0o644, Size: int64(len(data)),
			ModTime: time.Unix(0, 0)}); err != nil {
			return nil, err
		}
		if _, err := tw.Write(data); err != nil {
			return nil, err
		}
		files++
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	if err := s.repo.SaveTPDomains(ctx, team, recs); err != nil {
		return nil, err
	}
	if files == 0 {
		return nil, fmt.Errorf("no file of the %d domains could be downloaded", len(rows))
	}
	res := &glossary.FetchResult{Data: archive.Bytes(), Filename: "translationproject-" + team + ".tar.gz",
		ETag: page.ETag, LastModified: page.LastModified}
	if failed > 0 { // read the page again next time, so the missing files are tried again
		res.ETag, res.LastModified = "", ""
	}
	return res, nil
}

// tpFile downloads one domain's file and prepares it (UTF-8, a template becomes an empty PO file)
func (s *Service) tpFile(ctx context.Context, d tpsite.Domain, team string) ([]byte, error) {
	res, err := s.fetcher.Fetch(ctx, d.FileURL, "", "")
	if err != nil {
		return nil, err
	}
	return tpsite.Prepare(res.Data, team, d.Template)
}

// TPDomains returns the Translation Project domains of a project's TP module groups by module (nil = none)
func (s *Service) TPDomains(ctx context.Context, p *models.Project) map[string]*models.TPDomain {
	groups, err := s.Groups(ctx, p)
	if err != nil {
		return nil
	}
	var out map[string]*models.TPDomain
	seen := map[string]bool{}
	for _, g := range groups {
		if !g.IsTP() || seen[g.TPTeam] {
			continue
		}
		seen[g.TPTeam] = true
		domains, err := s.repo.TPDomains(ctx, g.TPTeam, false)
		if err != nil {
			log.Printf("translation project: domains of %s: %v", g.TPTeam, err)
			continue
		}
		if out == nil {
			out = map[string]*models.TPDomain{}
		}
		for _, d := range domains {
			out[tpKey(d.Team, models.ModuleKey(tpsite.FileName(d.Domain, d.Team)))] = d
		}
	}
	return out
}

func tpKey(team, module string) string { return team + "\x00" + module }

// attachTP fills UpstreamInfo.TP of project files and members' files whose module is a domain of a
// Translation Project team the project syncs from (same language as the team)
func (s *Service) attachTP(ctx context.Context, p *models.Project, files []*models.ProjectFile, catalogs []*models.Catalog) {
	if len(files) == 0 && len(catalogs) == 0 {
		return
	}
	domains := s.TPDomains(ctx, p)
	if domains == nil {
		return
	}
	teams := map[string]bool{}
	for _, d := range domains {
		teams[d.Team] = true
	}
	find := func(language, filename string) *models.TPDomain {
		for team := range teams {
			if po.SameLanguage(team, language) {
				if hit := domains[tpKey(team, models.ModuleKey(filename))]; hit != nil {
					return hit
				}
			}
		}
		return nil
	}
	for _, f := range files {
		f.TP = find(f.Language, f.Filename)
	}
	for _, c := range catalogs {
		if !c.IsTemplate {
			c.TP = find(c.Language, c.Filename)
		}
	}
}

// TPSubmit is a member's file prepared for the Translation Project's robot: the exported file with
// the name (and Project-Id-Version) the robot expects
func TPSubmit(pc *po.Catalog, d *models.TPDomain) (string, []byte) {
	pc.Header = po.SetHeaderField(pc.Header, "Project-Id-Version", d.Domain+" "+d.Version)
	return d.SubmitName(), pc.Bytes()
}

// ErrNotTP is returned for a file whose module is no Translation Project domain
var ErrNotTP = errors.New("not a translation project domain")

// TPFor returns the Translation Project domain of a member's file (ErrNotTP when there is none)
func (s *Service) TPFor(ctx context.Context, p *models.Project, c *models.Catalog) (*models.TPDomain, error) {
	s.attachTP(ctx, p, nil, []*models.Catalog{c})
	if c.TP == nil {
		return nil, ErrNotTP
	}
	return c.TP, nil
}
