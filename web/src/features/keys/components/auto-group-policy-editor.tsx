import { Plus, Trash2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { MultiSelect } from '@/components/multi-select'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  FormControl,
  FormDescription,
  FormItem,
  FormLabel,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

import {
  EMPTY_AUTO_GROUP_RULE,
  type AutoGroupModelRuleForm,
  type AutoGroupPolicyForm,
  type AutoGroupRuleForm,
} from '../lib/api-key-form'

type AutoGroupPolicyEditorProps = {
  value: AutoGroupPolicyForm
  onChange: (value: AutoGroupPolicyForm) => void
  autoGroups: string[]
  groupRatios: Record<string, number | string>
  models: string[]
}

type RuleFieldsProps = {
  rule: AutoGroupRuleForm
  onChange: (rule: AutoGroupRuleForm) => void
  autoGroups: string[]
  groupRatios: Record<string, number | string>
}

function RuleFields(props: RuleFieldsProps) {
  const { t } = useTranslation()
  const groupOptions = props.autoGroups.map((group) => ({
    value: group,
    label:
      typeof props.groupRatios[group] === 'number'
        ? `${group} (x${props.groupRatios[group]})`
        : group,
  }))
  const update = (patch: Partial<AutoGroupRuleForm>) =>
    props.onChange({ ...props.rule, ...patch })

  return (
    <div className='grid gap-3'>
      <FormItem>
        <FormLabel>{t('Group filter')}</FormLabel>
        <Select
          items={[
            { value: 'none', label: t('No group filter') },
            { value: 'allowlist', label: t('Allowlisted groups only') },
            { value: 'denylist', label: t('Denylisted groups') },
          ]}
          value={props.rule.mode}
          onValueChange={(value) =>
            update({
              mode: value as AutoGroupRuleForm['mode'],
              groups: value === 'none' ? [] : props.rule.groups,
            })
          }
        >
          <FormControl>
            <SelectTrigger className='w-full sm:w-64'>
              <SelectValue />
            </SelectTrigger>
          </FormControl>
          <SelectContent alignItemWithTrigger={false}>
            <SelectGroup>
              <SelectItem value='none'>{t('No group filter')}</SelectItem>
              <SelectItem value='allowlist'>
                {t('Allowlisted groups only')}
              </SelectItem>
              <SelectItem value='denylist'>{t('Denylisted groups')}</SelectItem>
            </SelectGroup>
          </SelectContent>
        </Select>
        <FormDescription>
          {t('Filter the administrator-ordered auto-group candidates.')}
        </FormDescription>
      </FormItem>

      {props.rule.mode !== 'none' && (
        <FormItem>
          <FormLabel>{t('Groups')}</FormLabel>
          <FormControl>
            <MultiSelect
              options={groupOptions}
              selected={props.rule.groups}
              onChange={(groups) => update({ groups })}
              placeholder={t('Select auto groups')}
              emptyText={t('No auto groups available')}
            />
          </FormControl>
        </FormItem>
      )}

      <div className='grid gap-3 sm:grid-cols-2'>
        <FormItem>
          <FormLabel>{t('Minimum ratio')}</FormLabel>
          <FormControl>
            <Input
              type='number'
              min={0}
              step='any'
              inputMode='decimal'
              placeholder={t('No minimum')}
              value={props.rule.min_ratio ?? ''}
              onChange={(event) =>
                update({
                  min_ratio:
                    event.target.value === ''
                      ? undefined
                      : event.target.valueAsNumber,
                })
              }
            />
          </FormControl>
        </FormItem>
        <FormItem>
          <FormLabel>{t('Maximum ratio')}</FormLabel>
          <FormControl>
            <Input
              type='number'
              min={0}
              step='any'
              inputMode='decimal'
              placeholder={t('No maximum')}
              value={props.rule.max_ratio ?? ''}
              onChange={(event) =>
                update({
                  max_ratio:
                    event.target.value === ''
                      ? undefined
                      : event.target.valueAsNumber,
                })
              }
            />
          </FormControl>
        </FormItem>
      </div>
    </div>
  )
}

export function AutoGroupPolicyEditor(props: AutoGroupPolicyEditorProps) {
  const { t } = useTranslation()
  const updateDefaultRule = (default_rule: AutoGroupRuleForm) =>
    props.onChange({ ...props.value, default_rule })
  const addModelRule = () => {
    const usedModels = new Set(
      props.value.model_rules.map((rule) => rule.model.trim())
    )
    const model =
      props.models.find((candidate) => !usedModels.has(candidate)) ?? ''
    props.onChange({
      ...props.value,
      model_rules: [
        ...props.value.model_rules,
        {
          id: crypto.randomUUID(),
          model,
          ...EMPTY_AUTO_GROUP_RULE,
          groups: [],
        },
      ],
    })
  }
  const updateModelRule = (
    index: number,
    patch: Partial<AutoGroupModelRuleForm>
  ) => {
    props.onChange({
      ...props.value,
      model_rules: props.value.model_rules.map((rule, ruleIndex) =>
        ruleIndex === index ? { ...rule, ...patch } : rule
      ),
    })
  }

  return (
    <div className='grid gap-4 rounded-lg border p-4'>
      <div className='space-y-1'>
        <p className='text-sm font-medium'>{t('Auto-group routing policy')}</p>
        <p className='text-muted-foreground text-xs'>
          {t(
            'These filters only apply when this API key uses the auto group. They cannot expand the administrator or user group permissions.'
          )}
        </p>
      </div>

      <FormItem className='flex items-center justify-between gap-4'>
        <div className='space-y-0.5'>
          <FormLabel>{t('Enable default auto-group policy')}</FormLabel>
          <FormDescription>
            {t('Use this policy for models without a model-specific rule.')}
          </FormDescription>
        </div>
        <FormControl>
          <Checkbox
            checked={props.value.enabled}
            onCheckedChange={(checked) =>
              props.onChange({ ...props.value, enabled: checked === true })
            }
            aria-label={t('Enable default auto-group policy')}
          />
        </FormControl>
      </FormItem>

      {props.value.enabled && (
        <div className='border-border border-y py-4'>
          <RuleFields
            rule={props.value.default_rule}
            onChange={updateDefaultRule}
            autoGroups={props.autoGroups}
            groupRatios={props.groupRatios}
          />
        </div>
      )}

      <div className='grid gap-3'>
        <div className='flex items-center justify-between gap-3'>
          <div className='space-y-1'>
            <p className='text-sm font-medium'>{t('Model-specific rules')}</p>
            <p className='text-muted-foreground text-xs'>
              {t('A model-specific rule overrides the default policy.')}
            </p>
          </div>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={addModelRule}
            disabled={props.models.length === 0}
          >
            <Plus data-icon='inline-start' />
            {t('Add model rule')}
          </Button>
        </div>

        {props.value.model_rules.length === 0 ? (
          <p className='text-muted-foreground text-xs'>
            {t('No model-specific rules configured.')}
          </p>
        ) : (
          <div className='divide-border divide-y border-y'>
            {props.value.model_rules.map((rule, index) => (
              <div key={rule.id} className='grid gap-3 py-4'>
                <div className='flex items-end gap-2'>
                  <FormItem className='min-w-0 flex-1'>
                    <FormLabel>{t('Model')}</FormLabel>
                    <FormControl>
                      <Input
                        list='auto-group-policy-models'
                        value={rule.model}
                        onChange={(event) =>
                          updateModelRule(index, { model: event.target.value })
                        }
                        placeholder={t('Enter a model name')}
                      />
                    </FormControl>
                  </FormItem>
                  <Button
                    type='button'
                    variant='ghost'
                    size='icon-sm'
                    title={t('Remove model rule')}
                    aria-label={t('Remove model rule')}
                    onClick={() =>
                      props.onChange({
                        ...props.value,
                        model_rules: props.value.model_rules.filter(
                          (_, ruleIndex) => ruleIndex !== index
                        ),
                      })
                    }
                  >
                    <Trash2 />
                  </Button>
                </div>
                <RuleFields
                  rule={rule}
                  onChange={(nextRule) => updateModelRule(index, nextRule)}
                  autoGroups={props.autoGroups}
                  groupRatios={props.groupRatios}
                />
              </div>
            ))}
          </div>
        )}
      </div>
      <datalist id='auto-group-policy-models'>
        {props.models.map((model) => (
          <option key={model} value={model} />
        ))}
      </datalist>
    </div>
  )
}
