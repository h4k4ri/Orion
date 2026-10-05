DO $$
BEGIN
    IF to_regclass('public.volumes') IS NOT NULL
       AND to_regclass('orion_volume.volumes') IS NULL THEN
        ALTER TABLE public.volumes SET SCHEMA orion_volume;
    END IF;
    IF to_regclass('public.processed_commands') IS NOT NULL
       AND to_regclass('orion_volume.processed_commands') IS NULL THEN
        ALTER TABLE public.processed_commands SET SCHEMA orion_volume;
    END IF;
END $$;
