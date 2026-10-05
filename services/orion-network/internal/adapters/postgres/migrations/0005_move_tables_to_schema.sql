DO $$
BEGIN
    IF to_regclass('public.networks') IS NOT NULL
       AND to_regclass('orion_network.networks') IS NULL THEN
        ALTER TABLE public.networks SET SCHEMA orion_network;
        ALTER TABLE public.subnets SET SCHEMA orion_network;
        ALTER TABLE public.ports SET SCHEMA orion_network;
    END IF;
    IF to_regclass('public.processed_commands') IS NOT NULL
       AND to_regclass('orion_network.processed_commands') IS NULL THEN
        ALTER TABLE public.processed_commands SET SCHEMA orion_network;
    END IF;
END $$;
