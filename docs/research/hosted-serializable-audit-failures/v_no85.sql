CREATE OR REPLACE FUNCTION mark_audit_write_in_flight() RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN NEW.recorded_at := clock_timestamp(); RETURN NEW; END; $$;
