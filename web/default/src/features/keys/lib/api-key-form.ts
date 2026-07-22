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
import type { TFunction } from 'i18next'
import { z } from 'zod'

import { validateModelMappingJson } from '@/features/channels/lib/model-mapping-validation'
import { parseQuotaFromDollars, quotaUnitsToDollars } from '@/lib/format'

import { DEFAULT_GROUP } from '../constants'
import type { ApiKey, ApiKeyFormData } from '../types'

export type AutoGroupPolicyMode = 'none' | 'allowlist' | 'denylist'

export type AutoGroupRuleForm = {
  mode: AutoGroupPolicyMode
  groups: string[]
  min_ratio?: number
  max_ratio?: number
}

export type AutoGroupModelRuleForm = AutoGroupRuleForm & {
  id: string
  model: string
}

export type AutoGroupPolicyForm = {
  enabled: boolean
  default_rule: AutoGroupRuleForm
  model_rules: AutoGroupModelRuleForm[]
}

export const EMPTY_AUTO_GROUP_RULE: AutoGroupRuleForm = {
  mode: 'none',
  groups: [],
  min_ratio: undefined,
  max_ratio: undefined,
}

export const EMPTY_AUTO_GROUP_POLICY: AutoGroupPolicyForm = {
  enabled: false,
  default_rule: EMPTY_AUTO_GROUP_RULE,
  model_rules: [],
}

function createEmptyAutoGroupPolicy(): AutoGroupPolicyForm {
  return {
    enabled: false,
    default_rule: { ...EMPTY_AUTO_GROUP_RULE, groups: [] },
    model_rules: [],
  }
}

// ============================================================================
// Form Schema
// ============================================================================

export function getApiKeyFormSchema(t: TFunction) {
  return z
    .object({
      name: z.string().min(1, t('Please enter a name')),
      remain_quota_dollars: z.number().optional(),
      expired_time: z.date().optional(),
      unlimited_quota: z.boolean(),
      model_limits: z.array(z.string()),
      model_mapping: z.string(),
      allow_ips: z.string().optional(),
      group: z.string().optional(),
      cross_group_retry: z.boolean().optional(),
      auto_group_policy: z.object({
        enabled: z.boolean(),
        default_rule: z.object({
          mode: z.enum(['none', 'allowlist', 'denylist']),
          groups: z.array(z.string()),
          min_ratio: z.number().finite().min(0).optional(),
          max_ratio: z.number().finite().min(0).optional(),
        }),
        model_rules: z.array(
          z.object({
            id: z.string(),
            model: z.string().trim().min(1),
            mode: z.enum(['none', 'allowlist', 'denylist']),
            groups: z.array(z.string()),
            min_ratio: z.number().finite().min(0).optional(),
            max_ratio: z.number().finite().min(0).optional(),
          })
        ),
      }),
      tokenCount: z.number().min(1).optional(),
    })
    .superRefine((data, ctx) => {
      const mappingValidation = validateModelMappingJson(data.model_mapping)
      if (!mappingValidation.valid) {
        ctx.addIssue({
          code: 'custom',
          path: ['model_mapping'],
          message: t('Invalid model mapping format'),
        })
      } else if (data.model_mapping.trim()) {
        const mapping = JSON.parse(data.model_mapping) as Record<string, string>
        const hasEmptyModel = Object.entries(mapping).some(
          ([source, target]) => !source.trim() || !target.trim()
        )
        if (hasEmptyModel) {
          ctx.addIssue({
            code: 'custom',
            path: ['model_mapping'],
            message: t('Invalid model mapping format'),
          })
        }
      }

      const allRules = [
        data.auto_group_policy.default_rule,
        ...data.auto_group_policy.model_rules,
      ]
      for (const rule of allRules) {
        if (
          rule.min_ratio !== undefined &&
          rule.max_ratio !== undefined &&
          rule.min_ratio > rule.max_ratio
        ) {
          ctx.addIssue({
            code: 'custom',
            path: ['auto_group_policy'],
            message: t('Minimum ratio must not exceed maximum ratio'),
          })
          break
        }
        if (rule.mode === 'none' && rule.groups.length > 0) {
          ctx.addIssue({
            code: 'custom',
            path: ['auto_group_policy'],
            message: t(
              'Choose an allowlist or denylist before selecting groups'
            ),
          })
          break
        }
      }

      const modelNames = data.auto_group_policy.model_rules.map((rule) =>
        rule.model.trim()
      )
      if (new Set(modelNames).size !== modelNames.length) {
        ctx.addIssue({
          code: 'custom',
          path: ['auto_group_policy'],
          message: t('Each model can have only one auto-group rule'),
        })
      }

      if (data.unlimited_quota) {
        return
      }

      if (
        data.remain_quota_dollars === undefined ||
        data.remain_quota_dollars < 0
      ) {
        ctx.addIssue({
          code: 'custom',
          path: ['remain_quota_dollars'],
          message: t('Quota must be zero or greater'),
        })
      }
    })
}

export type ApiKeyFormValues = z.infer<ReturnType<typeof getApiKeyFormSchema>>

// ============================================================================
// Form Defaults
// ============================================================================

export const API_KEY_FORM_DEFAULT_VALUES: ApiKeyFormValues = {
  name: '',
  remain_quota_dollars: 10,
  expired_time: undefined,
  unlimited_quota: true,
  model_limits: [],
  model_mapping: '',
  allow_ips: '',
  group: DEFAULT_GROUP,
  cross_group_retry: true,
  auto_group_policy: EMPTY_AUTO_GROUP_POLICY,
  tokenCount: 1,
}

export function getApiKeyFormDefaultValues(
  defaultUseAutoGroup: boolean
): ApiKeyFormValues {
  return {
    ...API_KEY_FORM_DEFAULT_VALUES,
    group: defaultUseAutoGroup ? 'auto' : DEFAULT_GROUP,
    cross_group_retry: defaultUseAutoGroup,
    auto_group_policy: createEmptyAutoGroupPolicy(),
  }
}

// ============================================================================
// Form Data Transformation
// ============================================================================

/**
 * Transform form data to API payload
 */
function serializeAutoGroupRule(rule: AutoGroupRuleForm) {
  const result: {
    mode?: 'allowlist' | 'denylist'
    groups?: string[]
    min_ratio?: number
    max_ratio?: number
  } = {}
  if (rule.mode !== 'none' && rule.groups.length > 0) {
    result.mode = rule.mode
    result.groups = rule.groups
  }
  if (rule.min_ratio !== undefined) result.min_ratio = rule.min_ratio
  if (rule.max_ratio !== undefined) result.max_ratio = rule.max_ratio
  return result
}

function ruleHasConstraint(rule: AutoGroupRuleForm): boolean {
  return (
    (rule.mode !== 'none' && rule.groups.length > 0) ||
    rule.min_ratio !== undefined ||
    rule.max_ratio !== undefined
  )
}

function serializeAutoGroupPolicy(policy: AutoGroupPolicyForm) {
  const modelRules: Record<
    string,
    ReturnType<typeof serializeAutoGroupRule>
  > = {}
  for (const rule of policy.model_rules) {
    if (ruleHasConstraint(rule)) {
      modelRules[rule.model.trim()] = serializeAutoGroupRule(rule)
    }
  }
  const defaultRule =
    policy.enabled && ruleHasConstraint(policy.default_rule)
      ? serializeAutoGroupRule(policy.default_rule)
      : undefined
  if (!defaultRule && Object.keys(modelRules).length === 0) return undefined

  return {
    default_rule: defaultRule,
    model_rules: modelRules,
  }
}

export function transformFormDataToPayload(
  data: ApiKeyFormValues
): ApiKeyFormData {
  const requestCustomization: {
    version: 1
    model_mapping?: Record<string, string>
  } = { version: 1 }
  if (data.model_mapping.trim()) {
    requestCustomization.model_mapping = JSON.parse(data.model_mapping)
  }
  const autoGroupPolicy = serializeAutoGroupPolicy(data.auto_group_policy)

  return {
    name: data.name,
    remain_quota: data.unlimited_quota
      ? 0
      : parseQuotaFromDollars(data.remain_quota_dollars || 0),
    expired_time: data.expired_time
      ? Math.floor(data.expired_time.getTime() / 1000)
      : -1,
    unlimited_quota: data.unlimited_quota,
    model_limits_enabled: data.model_limits.length > 0,
    model_limits: data.model_limits.join(','),
    auto_group_policy: autoGroupPolicy ? JSON.stringify(autoGroupPolicy) : '',
    request_customization: requestCustomization.model_mapping
      ? JSON.stringify(requestCustomization)
      : '',
    allow_ips: data.allow_ips || '',
    group: data.group || '',
    cross_group_retry: data.group === 'auto' ? !!data.cross_group_retry : false,
  }
}

function parseAutoGroupRule(raw: unknown): AutoGroupRuleForm {
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) {
    return { ...EMPTY_AUTO_GROUP_RULE, groups: [] }
  }
  const value = raw as {
    mode?: unknown
    groups?: unknown
    min_ratio?: unknown
    max_ratio?: unknown
  }
  const mode =
    value.mode === 'allowlist' || value.mode === 'denylist'
      ? value.mode
      : 'none'
  const minRatio =
    typeof value.min_ratio === 'number' &&
    Number.isFinite(value.min_ratio) &&
    value.min_ratio >= 0
      ? value.min_ratio
      : undefined
  const maxRatio =
    typeof value.max_ratio === 'number' &&
    Number.isFinite(value.max_ratio) &&
    value.max_ratio >= 0
      ? value.max_ratio
      : undefined
  return {
    mode,
    groups: Array.isArray(value.groups)
      ? value.groups.filter(
          (group): group is string => typeof group === 'string'
        )
      : [],
    min_ratio: minRatio,
    max_ratio: maxRatio,
  }
}

function extractTokenModelMapping(
  requestCustomization: string | null | undefined
): string {
  if (!requestCustomization?.trim()) return ''

  try {
    const config = JSON.parse(requestCustomization) as {
      version?: unknown
      model_mapping?: unknown
    }
    if (
      config.version !== 1 ||
      !config.model_mapping ||
      typeof config.model_mapping !== 'object' ||
      Array.isArray(config.model_mapping)
    ) {
      return ''
    }
    return JSON.stringify(config.model_mapping, null, 2)
  } catch {
    return ''
  }
}

function extractAutoGroupPolicy(
  autoGroupPolicy: string | null | undefined
): AutoGroupPolicyForm {
  const fallback: AutoGroupPolicyForm = {
    enabled: false,
    default_rule: { ...EMPTY_AUTO_GROUP_RULE, groups: [] },
    model_rules: [],
  }
  if (!autoGroupPolicy?.trim()) return fallback

  try {
    const policy = JSON.parse(autoGroupPolicy) as {
      default_rule?: unknown
      model_rules?: unknown
    }
    const rawRules = policy.model_rules
    const modelRules: AutoGroupModelRuleForm[] = []
    if (rawRules && typeof rawRules === 'object' && !Array.isArray(rawRules)) {
      for (const [model, rule] of Object.entries(rawRules)) {
        modelRules.push({
          id: model,
          model,
          ...parseAutoGroupRule(rule),
        })
      }
    }
    return {
      enabled: policy.default_rule !== undefined,
      default_rule: parseAutoGroupRule(policy.default_rule),
      model_rules: modelRules,
    }
  } catch {
    return fallback
  }
}

/**
 * Transform API key data to form defaults
 */
export function transformApiKeyToFormDefaults(
  apiKey: ApiKey
): ApiKeyFormValues {
  return {
    name: apiKey.name,
    remain_quota_dollars: apiKey.unlimited_quota
      ? 0
      : quotaUnitsToDollars(apiKey.remain_quota),
    expired_time:
      apiKey.expired_time > 0
        ? new Date(apiKey.expired_time * 1000)
        : undefined,
    unlimited_quota: apiKey.unlimited_quota,
    model_limits: apiKey.model_limits
      ? apiKey.model_limits.split(',').filter(Boolean)
      : [],
    model_mapping: extractTokenModelMapping(apiKey.request_customization),
    auto_group_policy: extractAutoGroupPolicy(apiKey.auto_group_policy),
    allow_ips: apiKey.allow_ips || '',
    group: apiKey.group || DEFAULT_GROUP,
    cross_group_retry: !!apiKey.cross_group_retry,
    tokenCount: 1,
  }
}
