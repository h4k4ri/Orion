DO $$
BEGIN
    IF to_regclass('public.operation_operations') IS NOT NULL
       AND to_regclass('orion_operation.operation_operations') IS NULL THEN
        ALTER TABLE public.operation_operations SET SCHEMA orion_operation;
        ALTER TABLE public.operation_operation_events SET SCHEMA orion_operation;
    END IF;
END $$;
