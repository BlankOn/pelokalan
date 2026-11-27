package review

import (
	"context"
	"log"
	"strings"

	"pelokalan/internal/catalog"
	"pelokalan/internal/models"
	"pelokalan/internal/platform/apperr"
	"pelokalan/internal/platform/i18n"
)

// "Gabungkan dengan upstream": merge a member's file with the newest upstream copy, keeping the member's unsent
// work and asking about strings changed on both sides (catalog.PlanUpstreamMerge / ApplyUpstreamMerge). The file's
// owner may do it, and owners and coordinators may do it for any member (e.g. when an approval is refused because
// upstream changed meanwhile). Nothing is sent to a repository here.

// CanMergeUpstream reports whether user may merge the file with upstream
func CanMergeUpstream(user *models.User, p *models.Project, c *models.Catalog) bool {
	if user == nil || c.IsTemplate || c.IsDeleted() || !p.Role.IsMember() {
		return false
	}
	return c.OwnerID == user.ID || p.Role.CanManage()
}

// UpstreamFile is the newest upstream copy of a member's file: the branch's file for repositories Pelokalan
// writes to, else the current PO file on the GNOME translation site when it has one, else the copy saved at the
// last sync. label says where it came from.
func (s *Service) UpstreamFile(ctx context.Context, p *models.Project, c *models.Catalog, maxBytes int64) ([]byte, string, error) {
	if g := s.DirectGroup(ctx, p, c); g != nil {
		if gh := s.glossary.GitHubApp(); gh != nil {
			f, err := gh.GetFile(ctx, g.GitInstallation, g.GitRepo, g.GitBranch, c.RepoPath)
			if err != nil {
				return nil, "", gitHubErr(err, c.RepoPath)
			}
			return f.Data, g.GitRepo + "@" + g.GitBranch, nil
		}
	}
	data, pf, err := s.glossary.UpstreamData(ctx, p, c)
	if err != nil {
		return nil, "", err
	}
	if pf == nil || data == nil {
		return nil, "", apperr.New("err.upstream_unavailable")
	}
	label := pf.GroupLabel
	if latest, from, err := s.glossary.UpstreamPO(ctx, p, pf, maxBytes); err == nil {
		data, label = latest, from
	}
	return data, label, nil
}

// PlanUpstreamMerge shows what merging the file with upstream would do (nothing changes)
func (s *Service) PlanUpstreamMerge(ctx context.Context, user *models.User, p *models.Project, c *models.Catalog, maxBytes int64) (*catalog.MergePlan, error) {
	if !CanMergeUpstream(user, p, c) {
		return nil, apperr.ErrForbidden
	}
	data, label, err := s.UpstreamFile(ctx, p, c, maxBytes)
	if err != nil {
		return nil, err
	}
	return s.catalogs.PlanUpstreamMerge(ctx, c, data, label, s.lastSent(ctx, c.ID))
}

// ApplyUpstreamMerge merges the file with upstream as planned (version from the plan; choices: conflict key →
// "mine" or "upstream"). When someone else merged the member's file, the member is emailed.
func (s *Service) ApplyUpstreamMerge(ctx context.Context, user *models.User, p *models.Project, c *models.Catalog, maxBytes int64, version string, choices map[string]string) (*catalog.MergePlan, error) {
	if !CanMergeUpstream(user, p, c) {
		return nil, apperr.ErrForbidden
	}
	data, label, err := s.UpstreamFile(ctx, p, c, maxBytes)
	if err != nil {
		return nil, err
	}
	plan, err := s.catalogs.ApplyUpstreamMerge(ctx, user, p.Role.CanManage(), c, data, label, version, s.lastSent(ctx, c.ID), choices)
	if err != nil {
		return nil, err
	}
	if c.OwnerID != user.ID {
		go s.notifyMerged(context.Background(), p, c, user, plan)
	}
	return plan, nil
}

// notifyMerged tells a member that someone merged their file with upstream
func (s *Service) notifyMerged(ctx context.Context, p *models.Project, c *models.Catalog, by *models.User, plan *catalog.MergePlan) {
	if s.mailer == nil || s.users == nil {
		return
	}
	u, err := s.users.GetByID(ctx, c.OwnerID)
	if err != nil {
		log.Printf("reviews: merge mail: %v", err)
		return
	}
	lang := i18n.Normalize(u.Lang)
	name := c.RepoPath
	if name == "" {
		name = c.Filename
	}
	subject := i18n.T(lang, "mail.merged_subject", name, p.Name)
	body := i18n.T(lang, "mail.merged_body", by.DisplayName(), name, p.Name, plan.Updated+plan.Added, plan.Kept, len(plan.Conflicts))
	s.send(ctx, u, lang, subject, body, "", strings.TrimRight(s.site.BaseURL, "/")+c.URL()+"/history",
		i18n.T(lang, "mail.merged_button"), p)
}
