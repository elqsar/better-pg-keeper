-- PostgreSQL initialization script for pganalyzer testing
-- This script runs on first database initialization

-- Enable pg_stat_statements extension
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;

-- Create a read-only user for monitoring (optional, for production use)
-- DO $$
-- BEGIN
--     IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'pganalyzer_monitor') THEN
--         CREATE USER pganalyzer_monitor WITH PASSWORD 'change_me_in_production';
--     END IF;
-- END
-- $$;

-- Grant necessary permissions to monitor user
-- GRANT pg_read_all_stats TO pganalyzer_monitor;
-- GRANT SELECT ON pg_stat_statements TO pganalyzer_monitor;

-- Drop existing tables (in correct order due to foreign keys)
DROP TABLE IF EXISTS order_items CASCADE;
DROP TABLE IF EXISTS orders CASCADE;
DROP TABLE IF EXISTS products CASCADE;
DROP TABLE IF EXISTS users CASCADE;

-- Create sample tables for testing
CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    username VARCHAR(100) NOT NULL UNIQUE,
    email VARCHAR(255) NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE orders (
    id SERIAL PRIMARY KEY,
    user_id INTEGER REFERENCES users(id),
    total_amount DECIMAL(10, 2),
    status VARCHAR(50) DEFAULT 'pending',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE products (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    price DECIMAL(10, 2),
    stock INTEGER DEFAULT 0,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE order_items (
    id SERIAL PRIMARY KEY,
    order_id INTEGER REFERENCES orders(id),
    product_id INTEGER REFERENCES products(id),
    quantity INTEGER DEFAULT 1,
    price DECIMAL(10, 2)
);

-- Create indexes
CREATE INDEX idx_orders_user_id ON orders(user_id);
CREATE INDEX idx_orders_status ON orders(status);
CREATE INDEX idx_order_items_order_id ON order_items(order_id);
CREATE INDEX idx_order_items_product_id ON order_items(product_id);

-- Insert sample data
INSERT INTO users (username, email) VALUES
    ('alice', 'alice@example.com'),
    ('bob', 'bob@example.com'),
    ('charlie', 'charlie@example.com');

INSERT INTO products (name, price, stock) VALUES
    ('Widget', 29.99, 100),
    ('Gadget', 49.99, 50),
    ('Gizmo', 19.99, 200);

-- Generate some query activity for pg_stat_statements
DO $$
DECLARE
    i INTEGER;
BEGIN
    FOR i IN 1..100 LOOP
        PERFORM * FROM users WHERE id = (i % 3) + 1;
        PERFORM * FROM products WHERE price > 20;
        PERFORM COUNT(*) FROM orders;
    END LOOP;
END $$;

-- Log successful initialization
DO $$
BEGIN
    RAISE NOTICE 'PostgreSQL initialized with pg_stat_statements enabled';
END $$;
