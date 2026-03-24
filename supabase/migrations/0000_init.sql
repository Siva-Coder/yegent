-- Enable pgvector extension
CREATE EXTENSION IF NOT EXISTS vector;

-- Workspaces Table (Tenants/Businesses)
CREATE TABLE workspaces (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    credits INTEGER DEFAULT 0,
    phone_number TEXT,
    language_preference TEXT DEFAULT 'Telugu/English'
);

-- Workspace Profiles (Business Details for the AI Prompt)
CREATE TABLE workspace_profiles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID REFERENCES workspaces(id) ON DELETE CASCADE,
    business_type TEXT NOT NULL,
    system_prompt TEXT NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Documents for RAG Knowledgebase
CREATE TABLE documents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID REFERENCES workspaces(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    content_type TEXT,
    storage_path TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Document Chunks and Embeddings
CREATE TABLE document_chunks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    document_id UUID REFERENCES documents(id) ON DELETE CASCADE,
    workspace_id UUID REFERENCES workspaces(id) ON DELETE CASCADE,
    content TEXT NOT NULL,
    metadata JSONB DEFAULT '{}'::jsonb,
    -- Adjust to 768 if using a native BGE / Sarvam model instead of OpenAI
    embedding vector(1536),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Create HNSW index for fast similarity search
CREATE INDEX ON document_chunks USING hnsw (embedding vector_cosine_ops);

-- Calls / Analytics Table
CREATE TABLE calls (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID REFERENCES workspaces(id) ON DELETE CASCADE,
    start_time TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    end_time TIMESTAMP WITH TIME ZONE,
    status TEXT DEFAULT 'in_progress', -- in_progress, completed, failed
    cost_credits INTEGER DEFAULT 0,
    transcript JSONB DEFAULT '[]'::jsonb
);

-- Seed Data (Andhra Pradesh Businesses)

-- 1. Real Estate Developer (Vizag)
INSERT INTO workspaces (id, name, credits, phone_number, language_preference) 
VALUES ('11111111-1111-1111-1111-111111111111', 'Sri Venkateswara Plots - Vizag', 500, '+919876543210', 'Telugu/English');

INSERT INTO workspace_profiles (workspace_id, business_type, system_prompt)
VALUES ('11111111-1111-1111-1111-111111111111', 'Real Estate', 'You are Kia, an AI receptionist for Sri Venkateswara Plots in Vizag. Answer inquiries about available residential plots, pricing, and schedule site visits. Speak politely in a mix of Telugu and English. Keep responses very brief and professional.');

-- 2. Regional Hospital (Amaravati/Vijayawada)
INSERT INTO workspaces (id, name, credits, phone_number, language_preference) 
VALUES ('22222222-2222-2222-2222-222222222222', 'Amaravati Care Hospitals - Vijayawada', 200, '+919876543211', 'Telugu/English');

INSERT INTO workspace_profiles (workspace_id, business_type, system_prompt)
VALUES ('22222222-2222-2222-2222-222222222222', 'Hospital', 'You are Kia, the AI appointment desk for Amaravati Care Hospitals in Vijayawada. Help patients book appointments with doctors (Cardiology, Orthopedics, General Medicine). Ask for their symptoms briefly. Converse in Telugu and English. Keep it empathetic and fast.');

-- 3. Coaching Center (Guntur)
INSERT INTO workspaces (id, name, credits, phone_number, language_preference) 
VALUES ('33333333-3333-3333-3333-333333333333', 'Chaitanya Group Coaching - Guntur', 300, '+919876543212', 'Telugu/English');

INSERT INTO workspace_profiles (workspace_id, business_type, system_prompt)
VALUES ('33333333-3333-3333-3333-333333333333', 'Education', 'You are Kia, an AI admission counselor for Chaitanya Group in Guntur. You provide information on JEE/NEET long-term batches, fee structure, and hostel facilities. Speak eagerly in Telugu and English. Persuade students to visit the campus.');

-- Enable Row Level Security (RLS)
ALTER TABLE workspaces ENABLE ROW LEVEL SECURITY;
ALTER TABLE workspace_profiles ENABLE ROW LEVEL SECURITY;
ALTER TABLE documents ENABLE ROW LEVEL SECURITY;
ALTER TABLE document_chunks ENABLE ROW LEVEL SECURITY;
ALTER TABLE calls ENABLE ROW LEVEL SECURITY;

-- Basic RLS Policies (Allow authenticated / service roles to manage resources)
CREATE POLICY "Enable all access" ON workspaces FOR ALL USING (true);
CREATE POLICY "Enable all access" ON workspace_profiles FOR ALL USING (true);
CREATE POLICY "Enable all access" ON documents FOR ALL USING (true);
CREATE POLICY "Enable all access" ON document_chunks FOR ALL USING (true);
CREATE POLICY "Enable all access" ON calls FOR ALL USING (true);
