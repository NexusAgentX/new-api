/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
/**
 * Zod schemas for common logs
 * This file should only contain Zod schemas and types inferred from them
 */
import { z } from 'zod'

// Raw exchange metadata attached to a usage log. The archive body is never
// included in log responses.
export const rawExchangeSummarySchema = z.object({
  request_id: z.string(),
  capture_mode: z.string(),
  outcome: z.string(),
  outcome_reason: z.string().optional(),
  status: z.string(),
  error_code: z.string().optional(),
  http_status: z.number().default(0),
  request_available: z.boolean().default(false),
  response_available: z.boolean().default(false),
  response_complete: z.boolean().default(false),
  request_bytes: z.number().default(0),
  response_bytes: z.number().default(0),
  request_stored_bytes: z.number().default(0),
  response_stored_bytes: z.number().default(0),
  created_at: z.number().default(0),
  expires_at: z.number().default(0),
  deleted_at: z.number().optional(),
})

export type RawExchangeSummary = z.infer<typeof rawExchangeSummarySchema>

// Usage log schema
export const usageLogSchema = z.object({
  id: z.number(),
  user_id: z.number(),
  created_at: z.number(),
  type: z.number(),
  content: z.string(),
  username: z.string().default(''),
  token_name: z.string().default(''),
  model_name: z.string().default(''),
  quota: z.number().default(0),
  quota_before_group: z.number().nullish(),
  quota_after_group_unrounded: z.number().nullish(),
  prompt_tokens: z.number().default(0),
  completion_tokens: z.number().default(0),
  use_time: z.number().default(0),
  is_stream: z.boolean().default(false),
  channel: z.number().default(0),
  channel_name: z.string().nullish().default(''),
  token_id: z.number().default(0),
  group: z.string().default(''),
  ip: z.string().default(''),
  other: z.string().default(''),
  request_id: z.string().default(''),
  upstream_request_id: z.string().default(''),
  raw_exchange: rawExchangeSummarySchema.nullish(),
})

export type UsageLog = z.infer<typeof usageLogSchema>
