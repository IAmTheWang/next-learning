CREATE TABLE accounts (
    id SERIAL PRIMARY KEY,
    name TEXT NOT NULL,
    balance NUMERIC(12, 2) NOT NULL DEFAULT 0
);

CREATE TABLE transactions (
    id SERIAL PRIMARY KEY,
    account_id INTEGER NOT NULL REFERENCES accounts(id),
    amount NUMERIC(12, 2) NOT NULL,
    description TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE budgets (
    id SERIAL PRIMARY KEY,
    category TEXT NOT NULL,
    limit_amount NUMERIC(12, 2) NOT NULL,
    spent_amount NUMERIC(12, 2) NOT NULL DEFAULT 0
);

INSERT INTO accounts (name, balance) VALUES
    ('メイン口座', 452300.00),
    ('貯蓄口座', 1200000.00);

INSERT INTO transactions (account_id, amount, description, created_at) VALUES
    (1, -3200.00, 'スーパー', now() - interval '1 day'),
    (1, -980.00, 'カフェ', now() - interval '2 day'),
    (1, 280000.00, '給与', now() - interval '5 day'),
    (1, -12000.00, '電気代', now() - interval '6 day'),
    (2, -50000.00, '定期預金へ振替', now() - interval '10 day');

INSERT INTO budgets (category, limit_amount, spent_amount) VALUES
    ('食費', 40000.00, 23400.00),
    ('交通費', 10000.00, 6200.00),
    ('娯楽', 15000.00, 15800.00);
