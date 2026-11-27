package service

import (
	"context"
	"errors"
	"log"

	"pelokalan/internal/models"
	"pelokalan/pkg/githubapp"
)

// Full integration with GitHub repositories (option 3 of the GitHub integration): the Pelokalan GitHub App is
// installed on a module group's repository, and approved modules are committed straight to its branch
// (ReviewService). Managers check the app's access and turn the integration on per module group.

// SetGitHub turns on the GitHub App (nil = not configured)
func (s *GlossaryService) SetGitHub(c *githubapp.Client) { s.github = c }

// GitHubApp is the configured GitHub App (nil = none)
func (s *GlossaryService) GitHubApp() *githubapp.Client { return s.github }

// CheckGitAccess asks GitHub whether the app is installed on the group's repository and may write it, and
// stores the answer (no write access also turns full integration off)
func (s *GlossaryService) CheckGitAccess(ctx context.Context, p *models.Project, groupID int64) (*models.ModuleGroup, error) {
	if !p.Role.CanManage() {
		return nil, ErrForbidden
	}
	g, err := s.projectGroup(ctx, p, groupID)
	if err != nil {
		return nil, err
	}
	if !g.IsGit() || s.github == nil {
		return nil, ErrNotFound
	}
	a, err := s.github.Check(ctx, g.GitRepo)
	msg := ""
	switch {
	case errors.Is(err, githubapp.ErrNotInstalled):
		msg = "err.github_not_installed"
	case err != nil:
		log.Printf("github: check %s: %v", g.GitRepo, err)
		msg = "err.github_unreachable"
	case !a.Write:
		msg = "err.github_no_write"
	}
	if err := s.repo.SetGroupGitAccess(ctx, g.ID, a.Installation, a.Write && err == nil, msg); err != nil {
		return nil, err
	}
	return s.repo.GetGroup(ctx, g.ID)
}

// SetGitDirect turns full integration with the group's repository on (after a fresh access check that
// must allow writing) or off
func (s *GlossaryService) SetGitDirect(ctx context.Context, p *models.Project, groupID int64, on bool) error {
	if !p.Role.CanManage() {
		return ErrForbidden
	}
	g, err := s.projectGroup(ctx, p, groupID)
	if err != nil {
		return err
	}
	if !g.IsGit() {
		return ErrNotFound
	}
	if on {
		if g, err = s.CheckGitAccess(ctx, p, groupID); err != nil {
			return err
		}
		if !g.GitWrite {
			return userErr(g.GitAccessError)
		}
	}
	return s.repo.SetGroupGitDirect(ctx, g.ID, on)
}
