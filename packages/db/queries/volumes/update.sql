-- name: UpdateVolumeStatus :one
UPDATE "public"."volumes"
SET status = @status,
    updated_at = NOW()
WHERE id = @id
RETURNING *;

-- name: UpdateVolumeStats :one
UPDATE "public"."volumes"
SET total_size_bytes = @total_size_bytes,
    total_file_count = @total_file_count,
    updated_at = NOW()
WHERE id = @id
RETURNING *;

-- name: DeleteVolume :exec
DELETE FROM "public"."volumes"
WHERE id = @id;
