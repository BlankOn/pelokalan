-- The reviewer role: translates like a translator and reviews (review comments), but does not approve or send
-- files to a repository.
ALTER TABLE project_members DROP CONSTRAINT IF EXISTS project_members_role_check;
ALTER TABLE project_members ADD CONSTRAINT project_members_role_check CHECK (role IN ('owner', 'manager', 'reviewer', 'translator'));
ALTER TABLE project_invitations DROP CONSTRAINT IF EXISTS project_invitations_role_check;
ALTER TABLE project_invitations ADD CONSTRAINT project_invitations_role_check CHECK (role IN ('manager', 'reviewer', 'translator'));
ALTER TABLE project_invite_links DROP CONSTRAINT IF EXISTS project_invite_links_role_check;
ALTER TABLE project_invite_links ADD CONSTRAINT project_invite_links_role_check CHECK (role IN ('manager', 'reviewer', 'translator'));
