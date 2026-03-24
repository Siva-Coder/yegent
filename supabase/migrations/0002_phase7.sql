-- Phase 7 Updates: Campaign Management & Post-Call Lead Extraction
ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS status TEXT DEFAULT 'active';
ALTER TABLE campaigns ADD COLUMN IF NOT EXISTS timeline_flow JSONB DEFAULT '[]'::jsonb;

CREATE TABLE IF NOT EXISTS collected_leads (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  campaign_id UUID REFERENCES campaigns(id) ON DELETE CASCADE,
  user_phone TEXT,
  extracted_data JSONB DEFAULT '{}'::jsonb,
  call_transcript TEXT,
  created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

ALTER TABLE collected_leads ENABLE ROW LEVEL SECURITY;

CREATE POLICY "Users manage own leads via campaigns" ON collected_leads
  FOR ALL USING (
    campaign_id IN (SELECT id FROM campaigns WHERE user_id = auth.uid())
  );
