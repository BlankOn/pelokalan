DELETE FROM users u
WHERE u.email = 'admin@blankon.id'
  AND NOT EXISTS (SELECT 1 FROM projects p WHERE p.owner_id = u.id);
