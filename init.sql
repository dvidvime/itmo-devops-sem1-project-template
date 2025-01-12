CREATE TABLE IF NOT EXISTS prices (
    id SERIAL PRIMARY KEY,
    name varchar NOT NULL,
    category varchar NOT NULL,
    price float8 NOT NULL,
    create_date timestamp DEFAULT CURRENT_TIMESTAMP NOT NULL
);