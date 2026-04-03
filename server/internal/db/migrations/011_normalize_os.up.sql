ALTER TABLE devices ADD COLUMN os_display TEXT NOT NULL DEFAULT '';

-- Preserve the original pretty OS name before normalizing.
UPDATE devices SET os_display = os WHERE os_display = '';

-- Normalize os to GOOS-style values for manifest matching.
UPDATE devices SET os = 'linux' WHERE os != 'linux' AND os != 'windows' AND os != 'darwin'
  AND (LOWER(os) LIKE '%linux%' OR LOWER(os) LIKE '%ubuntu%' OR LOWER(os) LIKE '%debian%'
       OR LOWER(os) LIKE '%fedora%' OR LOWER(os) LIKE '%centos%' OR LOWER(os) LIKE '%rhel%'
       OR LOWER(os) LIKE '%arch%' OR LOWER(os) LIKE '%alpine%');
UPDATE devices SET os = 'windows' WHERE os != 'windows' AND LOWER(os) LIKE '%windows%';
UPDATE devices SET os = 'darwin' WHERE os != 'darwin'
  AND (LOWER(os) LIKE '%darwin%' OR LOWER(os) LIKE '%macos%');
