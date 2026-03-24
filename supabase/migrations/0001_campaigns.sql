-- Campaigns table for multi-tenant AI configuration
CREATE TABLE IF NOT EXISTS campaigns (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID REFERENCES auth.users(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  persona TEXT NOT NULL,
  objective TEXT NOT NULL,
  greeting TEXT NOT NULL,
  language_preference TEXT NOT NULL DEFAULT 'te-IN',
  created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

ALTER TABLE campaigns ENABLE ROW LEVEL SECURITY;

CREATE POLICY "Users manage own campaigns" ON campaigns
  FOR ALL USING (auth.uid() = user_id);
