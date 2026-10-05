DO $$
BEGIN
    IF to_regclass('public.placement_hosts') IS NOT NULL
       AND to_regclass('orion_placement.placement_hosts') IS NULL THEN
        ALTER TABLE public.placement_hosts SET SCHEMA orion_placement;
    END IF;
    IF to_regclass('public.placement_reservations') IS NOT NULL
       AND to_regclass('orion_placement.placement_reservations') IS NULL THEN
        ALTER TABLE public.placement_reservations SET SCHEMA orion_placement;
    END IF;
    IF to_regclass('public.processed_commands') IS NOT NULL
       AND to_regclass('orion_placement.processed_commands') IS NULL THEN
        ALTER TABLE public.processed_commands SET SCHEMA orion_placement;
    END IF;
END $$;
