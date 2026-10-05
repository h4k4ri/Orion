DO $$
BEGIN
    IF to_regclass('public.compute_servers') IS NOT NULL
       AND to_regclass('orion_compute.compute_servers') IS NULL THEN
        ALTER TABLE public.compute_servers SET SCHEMA orion_compute;
        ALTER TABLE public.compute_tasks SET SCHEMA orion_compute;
    END IF;
END $$;
