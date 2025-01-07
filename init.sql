CREATE TABLE IF NOT EXISTS prices (
    id integer NOT NULL,
    name varchar NOT NULL,
    category varchar NOT NULL,
    price float8 NOT NULL,
    create_date timestamp DEFAULT CURRENT_TIMESTAMP NOT NULL,
    CONSTRAINT prices_pk PRIMARY KEY (id)
);