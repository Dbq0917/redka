# Persistence

Redka stores data in a SQL database using the following tables:

```
rkey
---
id       integer primary key
key      text not null
type     integer not null    -- 1 string, 2 list, 3 set, 4 hash, 5 sorted set
version  integer not null    -- incremented when the key value is updated
etime    integer             -- expiration timestamp in unix milliseconds
mtime    integer not null    -- modification timestamp in unix milliseconds
len      integer             -- number of child elements

rstring
---
kid      integer not null    -- FK -> rkey.id
value    blob not null

rlist
---
kid      integer not null    -- FK -> rkey.id
pos      real not null       -- is used for ordering, but is not an index
elem     blob not null

rset
---
kid      integer not null    -- FK -> rkey.id
elem     blob not null

rhash
---
kid      integer not null    -- FK -> rkey.id
field    text not null
value    blob not null

rzset
---
kid      integer not null    -- FK -> rkey.id
elem     blob not null
score    real not null
```

To access the data with SQL, use views instead of tables:

```sql
select * from vstring;
```

```
┌─────┬──────┬───────┬───────┬─────────────────────┐
│ kid │ key  │ value │ etime │        mtime        │
├─────┼──────┼───────┼───────┼─────────────────────┤
│ 1   │ name │ alice │       │ 2024-04-03 16:58:14 │
│ 2   │ age  │ 50    │       │ 2024-04-03 16:34:52 │
└─────┴──────┴───────┴───────┴─────────────────────┘
```

`etime` and `mtime` are in UTC.

There is a separate view for every data type:

```
vkey  vstring  vlist  vset  vhash  vzset
```

## Logical consistency check

Database-level integrity checks validate pages, but they cannot detect
logical mismatches between `rkey` and the type-specific tables. Use
`DB.Check` to find keys without data, data without keys, data stored under
the wrong key type, and incorrect `len` values:

```go
report, err := db.Check()
if err != nil {
    log.Fatal(err)
}
log.Info("inconsistencies", "total", report.Total)
for ktype, detail := range report.ByType {
    if detail.Total > 0 {
        log.Info("inconsistent keys", "type", ktype, "count", detail.Total)
    }
}
```

The check uses the read-only database handle and does not take a write lock.
It returns aggregate counts only, so its memory use does not grow with the
number of keys.

Use `DB.Repair` to delete orphaned or misplaced data, remove keys without
data, and rebuild collection lengths:

```go
fixed, err := db.Repair()
if err != nil {
    log.Fatal(err)
}
log.Info("repaired records", "total", fixed.Total)
```

Repair is idempotent. Running it again after a successful repair makes no
changes.
