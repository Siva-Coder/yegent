-- Phase 17: Public Users Sync & Outbound Leads CRM

-- 1. Create a public users table to store metadata (like credits)
CREATE TABLE IF NOT EXISTS public.users (
  id UUID PRIMARY KEY REFERENCES auth.users(id) ON DELETE CASCADE,
  available_credits INTEGER DEFAULT 100,
  created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Enable RLS on users
ALTER TABLE public.users ENABLE ROW LEVEL SECURITY;
CREATE POLICY "Users can view their own profile" ON public.users
  FOR SELECT USING (auth.uid() = id);

-- 2. Trigger to automatically create a public.users record when a new Auth user signs up
CREATE OR REPLACE FUNCTION public.handle_new_user()
RETURNS TRIGGER AS $$
BEGIN
  INSERT INTO public.users (id, available_credits)
  VALUES (NEW.id, 100);
  RETURN NEW;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER;

-- Trigger security: only system can trigger
DROP TRIGGER IF EXISTS on_auth_user_created ON auth.users;
CREATE TRIGGER on_auth_user_created
  AFTER INSERT ON auth.users
  FOR EACH ROW EXECUTE PROCEDURE public.handle_new_user();

-- 3. Create the Outbound Leads table (For CRM management)
CREATE TABLE IF NOT EXISTS leads (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID REFERENCES public.users(id) ON DELETE CASCADE,
  campaign_id UUID REFERENCES campaigns(id) ON DELETE SET NULL,
  name TEXT,
  phone TEXT,
  email TEXT,
  summary TEXT,
  ai_contacted BOOLEAN DEFAULT false,
  created_at TIMESTAMPTZ DEFAULT NOW()
);

-- 4. Add user_id to documents for multi-tenant isolation
ALTER TABLE documents ADD COLUMN IF NOT EXISTS user_id UUID REFERENCES public.users(id);

-- 5. Update Campaigns RLS to allow viewing legacy (ownerless) campaigns
DROP POLICY IF EXISTS "Users manage own campaigns" ON campaigns;
CREATE POLICY "Users manage own campaigns" ON campaigns
  FOR ALL USING (auth.uid() = user_id OR user_id IS NULL);

-- Enable RLS on leads
ALTER TABLE leads ENABLE ROW LEVEL SECURITY;
CREATE POLICY "Users manage own leads" ON leads
  FOR ALL USING (auth.uid() = user_id);

-- 4. Ensure campaigns are linked to public.users for consistency (optional but recommended)
-- It already references auth.users which is the same ID.

-- 5. Seed initial users from existing auth.users if any (for migration safety)
INSERT INTO public.users (id, available_credits)
SELECT id, 100 FROM auth.users
ON CONFLICT (id) DO NOTHING;
