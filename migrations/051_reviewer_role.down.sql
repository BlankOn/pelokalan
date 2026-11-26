UPDATE project_members SET role = 'translator' WHERE role = 'reviewer';
UPDATE project_invitations SET role = 'translator' WHERE role = 'reviewer';
UPDATE project_invite_links SET role = 'translator' WHERE role = 'reviewer';
ALTER TABLE project_members DROP CONSTRAINT IF EXISTS project_members_role_check;
ALTER TABLE project_members ADD CONSTRAINT project_members_role_check CHECK (role IN ('owner', 'manager', 'translator'));
ALTER TABLE project_invitations DROP CONSTRAINT IF EXISTS project_invitations_role_check;
ALTER TABLE project_invitations ADD CONSTRAINT project_invitations_role_check CHECK (role IN ('manager', 'translator'));
ALTER TABLE project_invite_links DROP CONSTRAINT IF EXISTS project_invite_links_role_check;
ALTER TABLE project_invite_links ADD CONSTRAINT project_invite_links_role_check CHECK (role IN ('manager', 'translator'));
