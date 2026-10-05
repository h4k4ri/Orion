INSERT INTO identity_users (id, username, password)
SELECT 'usr_admin', 'admin', 'orion-admin'
WHERE NOT EXISTS (
	SELECT 1 FROM identity_users WHERE username = 'admin'
);

INSERT INTO identity_projects (id, name)
VALUES ('proj_admin', 'admin')
ON CONFLICT (id) DO NOTHING;

INSERT INTO identity_user_project_roles (user_id, project_id, role)
SELECT user_id, 'proj_admin', role
FROM (
	SELECT id AS user_id FROM identity_users WHERE username = 'admin'
) admin_user
CROSS JOIN (
	VALUES ('admin'), ('member'), ('reader')
) roles(role)
ON CONFLICT (user_id, project_id, role) DO NOTHING;

INSERT INTO identity_user_system_roles (user_id, role)
SELECT user_id, role
FROM (
	SELECT id AS user_id FROM identity_users WHERE username = 'admin'
) admin_user
CROSS JOIN (
	VALUES ('admin'), ('member'), ('reader')
) roles(role)
ON CONFLICT (user_id, role) DO NOTHING;
