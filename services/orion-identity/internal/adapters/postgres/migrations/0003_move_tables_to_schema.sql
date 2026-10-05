DO $$
BEGIN
    IF to_regclass('public.identity_users') IS NOT NULL
       AND to_regclass('orion_identity.identity_users') IS NULL THEN
        ALTER TABLE public.identity_users SET SCHEMA orion_identity;
        ALTER TABLE public.identity_projects SET SCHEMA orion_identity;
        ALTER TABLE public.identity_user_project_roles SET SCHEMA orion_identity;
        ALTER TABLE public.identity_user_system_roles SET SCHEMA orion_identity;
        ALTER TABLE public.identity_tokens SET SCHEMA orion_identity;
    END IF;
END $$;
