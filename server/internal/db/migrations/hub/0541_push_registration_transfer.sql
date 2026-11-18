-- A re-paired phone keeps its push registration id but registers with a new credential. Before this, the fence
-- refused any change of owner on a live row, so the registration stayed under the old credential, and revoking that
-- credential later (it looks unused) retired the push registration of the phone still in use.
--
-- Same function as 0435 plus one allowance: an authenticated registration (`persona.push_transfer` set to this
-- device id by UpsertDevice) may move a live row to another live credential when it proves the same installation:
-- the same topic and environment, and the same push key or the same APNs token. That is the proof 0435 already
-- accepts for re-registering after the old credential was revoked. The old credential must also not have been seen
-- since the new one was created. Revoking the credential that now owns the row still retires it, so revoking a
-- device still stops its pushes.
CREATE OR REPLACE FUNCTION reject_retired_push_device() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    retired_reason text;
    retired_owner text;
    new_owner text;
    current_owner text;
    retired_key text;
    retired_target text;
    retired_topic text;
    retired_env text;
    retired_time timestamptz;
    transferring boolean := false;
BEGIN
    PERFORM pg_advisory_xact_lock(x'706572736f6e61'::bigint);
    SELECT reason, credential_id, push_key_hash, target_hash, topic, apns_env, retired_at
        INTO retired_reason, retired_owner, retired_key, retired_target, retired_topic, retired_env, retired_time
        FROM push_device_retirements WHERE device_id = NEW.device_id;
    IF TG_TABLE_NAME = 'devices' THEN
        new_owner := COALESCE(NULLIF(NEW.credential_id, ''), NEW.credential);
        SELECT COALESCE(NULLIF(credential_id, ''), credential) INTO current_owner
            FROM devices WHERE device_id = NEW.device_id;
        IF TG_OP = 'UPDATE' AND current_setting('persona.push_writer', true) IS DISTINCT FROM NEW.device_id
            AND (NEW.apns_token IS DISTINCT FROM OLD.apns_token OR NEW.relay_url IS DISTINCT FROM OLD.relay_url
                OR NEW.topic IS DISTINCT FROM OLD.topic OR NEW.apns_env IS DISTINCT FROM OLD.apns_env
                OR NEW.push_key IS DISTINCT FROM OLD.push_key) THEN
            RAISE EXCEPTION 'push registration ownership changed' USING ERRCODE = '23514';
        END IF;
        -- A re-paired installation moves its row to its new, live credential, once the old credential has not been
        -- used since the new one was issued (re-pairing switches credentials; two credentials both in use do not).
        IF TG_OP = 'UPDATE' AND current_setting('persona.push_transfer', true) = NEW.device_id
            AND new_owner <> ''
            AND EXISTS (SELECT 1 FROM device_tokens WHERE id = new_owner AND revoked_at IS NULL)
            AND NOT EXISTS (SELECT 1 FROM device_tokens old_owner, device_tokens new_cred
                WHERE old_owner.id = current_owner AND new_cred.id = new_owner
                    AND old_owner.last_seen IS NOT NULL AND old_owner.last_seen > new_cred.created_at)
            AND OLD.topic <> '' AND NEW.topic = OLD.topic AND NEW.apns_env = OLD.apns_env
            AND ((OLD.push_key <> '' AND NEW.push_key = OLD.push_key)
                OR (OLD.apns_token <> '' AND NEW.apns_token = OLD.apns_token)) THEN
            transferring := true;
        END IF;
        -- INSERT ... ON CONFLICT prefs writes may present another caller's
        -- credential but retain the existing owner in the actual UPDATE row.
        IF (TG_OP = 'UPDATE' AND current_owner <> '' AND current_owner <> new_owner AND NOT transferring)
            OR ((TG_OP = 'UPDATE' OR current_owner IS NULL)
                AND EXISTS (SELECT 1 FROM device_tokens WHERE id = new_owner AND revoked_at IS NOT NULL)) THEN
            RAISE EXCEPTION 'push registration ownership changed' USING ERRCODE = '23514';
        END IF;
    END IF;
    IF retired_reason IS NOT NULL THEN
        IF TG_TABLE_NAME <> 'devices' THEN
            RAISE EXCEPTION 'push registration was superseded' USING ERRCODE = '23514';
        END IF;
        -- Authenticated enrollment can restore an inactive/invalid same-owner ID.
        -- Re-pairing needs a different live owner and scoped installation proof.
        -- A stale slot or background save never may restore a retirement.
        IF TG_TABLE_NAME = 'devices'
            AND current_setting('persona.push_enrollment', true) = NEW.device_id
            AND (
                (retired_reason = 'superseded' AND retired_owner <> '' AND retired_owner = new_owner
                    AND NEW.registered_at > retired_time
                    AND EXISTS (SELECT 1 FROM devices candidate
                        WHERE COALESCE(NULLIF(candidate.credential_id, ''), candidate.credential) = new_owner
                            AND candidate.topic <> '' AND candidate.topic = NEW.topic AND candidate.apns_env = NEW.apns_env
                            AND ((candidate.push_key <> '' AND candidate.push_key = NEW.push_key)
                                OR (candidate.apns_token <> '' AND candidate.apns_token = NEW.apns_token))))
                OR (retired_reason IN ('inactive', 'invalid_token') AND (retired_owner = '' OR retired_owner = new_owner))
                OR (retired_reason IN ('inactive', 'invalid_token', 'revoked') AND new_owner <> retired_owner
                    AND retired_topic <> '' AND retired_topic = NEW.topic AND retired_env = NEW.apns_env
                    AND ((retired_key <> '' AND retired_key = encode(sha256(convert_to(NEW.push_key, 'UTF8')), 'hex'))
                        OR (retired_target <> '' AND retired_target = encode(sha256(convert_to(NEW.apns_token, 'UTF8')), 'hex'))))
            ) THEN
            DELETE FROM push_device_retirements WHERE device_id = NEW.device_id;
        ELSE
            RAISE EXCEPTION 'push registration was superseded' USING ERRCODE = '23514';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
