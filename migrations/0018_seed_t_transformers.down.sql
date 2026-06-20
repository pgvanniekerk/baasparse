-- Roll back T_TRANSFORMER seed entries.
DELETE FROM T_TRANSFORMER
WHERE (T_FROM_DT_UID, T_TO_DT_UID) IN (
    SELECT f.DT_UID, t.DT_UID
    FROM DT_DATA_TYPE f, DT_DATA_TYPE t
    WHERE (f.DT_CODE, t.DT_CODE) IN (
        ('DSV',        'MESSAGEPACK'),
        ('MESSAGEPACK','DSV'),
        ('JSON',       'MESSAGEPACK'),
        ('MESSAGEPACK','JSON')
    )
);
