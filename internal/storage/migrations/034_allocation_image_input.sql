-- Settled allowance entries keep the image share of input so manual retries compare the exact
-- breakdown that determined the charge.
ALTER TABLE allocation_entries ADD COLUMN image_input_tokens INTEGER NOT NULL DEFAULT 0 CHECK(image_input_tokens>=0);
