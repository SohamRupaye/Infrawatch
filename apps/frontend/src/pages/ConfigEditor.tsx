import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import * as Dialog from '@radix-ui/react-dialog'
import { Plus, Pencil, Trash2, X, Settings2, Wrench } from 'lucide-react'
import { toast } from 'sonner'
import {
  fetchConfigServices,
  createConfigService,
  updateConfigService,
  deleteConfigService,
} from '@/api/config'
import { Spinner } from '@/components/Spinner'
import { EmptyState } from '@/components/EmptyState'
import { formatNsDuration } from '@/lib/utils'
import type { ServiceConfig, ServiceConfigInput } from '@/api/types'

const HEALING_OPTIONS = ['docker_restart', 'kubectl_restart', 'fallback', 'webhook']

const EMPTY_FORM: ServiceConfigInput = {
  name: '',
  url: '',
  interval: '30s',
  timeout: '5s',
  tags: [],
  dependencies: [],
  healing_actions: [],
}

function ServiceForm({
  initial,
  onSave,
  saving,
}: {
  initial: ServiceConfigInput
  onSave: (v: ServiceConfigInput) => void
  saving: boolean
}) {
  const [form, setForm] = useState<ServiceConfigInput>(initial)

  function field(key: keyof ServiceConfigInput, label: string, type = 'text') {
    const value = form[key]
    return (
      <div key={key} className="flex flex-col gap-1">
        <label className="text-xs font-extrabold uppercase" style={{ color: 'var(--text-2)' }}>{label}</label>
        <input
          type={type}
          value={typeof value === 'string' ? value : (typeof value === 'number' ? String(value) : '')}
          onChange={(e) => setForm((f) => ({ ...f, [key]: e.target.value }))}
          className="field"
        />
      </div>
    )
  }

  function tagsField(key: 'tags' | 'dependencies' | 'healing_actions', label: string) {
    const values = (form[key] ?? []) as string[]
    return (
      <div key={key} className="flex flex-col gap-1">
        <label className="text-xs font-extrabold uppercase" style={{ color: 'var(--text-2)' }}>{label}</label>
        {key === 'healing_actions' ? (
          <div className="flex flex-wrap gap-2">
            {HEALING_OPTIONS.map((opt) => (
              <label key={opt} className="tag gap-2">
                <input
                  type="checkbox"
                  checked={values.includes(opt)}
                  onChange={(e) => {
                    const next = e.target.checked
                      ? [...values, opt]
                      : values.filter((v) => v !== opt)
                    setForm((f) => ({ ...f, healing_actions: next }))
                  }}
                  className="accent-[#4f46e5]"
                />
                {opt}
              </label>
            ))}
          </div>
        ) : (
          <input
            type="text"
            placeholder="comma-separated"
            value={values.join(', ')}
            onChange={(e) =>
              setForm((f) => ({
                ...f,
                [key]: e.target.value
                  .split(',')
                  .map((s) => s.trim())
                  .filter(Boolean),
              }))
            }
            className="field"
          />
        )}
      </div>
    )
  }

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault()
        onSave(form)
      }}
      className="space-y-3"
    >
      {field('name', 'Name *')}
      {field('url', 'URL *')}
      <div className="grid grid-cols-2 gap-3">
        {field('interval', 'Interval (e.g. 30s)')}
        {field('timeout', 'Timeout (e.g. 5s)')}
      </div>
      {field('method', 'HTTP Method')}
      {tagsField('tags', 'Tags')}
      {tagsField('dependencies', 'Dependencies')}
      <div className="grid grid-cols-2 gap-3">
        {field('container_name', 'Container name')}
        {field('fallback_url', 'Fallback URL')}
      </div>
      {tagsField('healing_actions', 'Healing actions')}
      <div className="flex justify-end pt-2">
        <button
          type="submit"
          disabled={saving || !form.name || !form.url}
          className="action-button action-primary disabled:opacity-40"
        >
          {saving ? 'Saving…' : 'Save'}
        </button>
      </div>
    </form>
  )
}

export function ConfigEditor() {
  const qc = useQueryClient()
  const [editTarget, setEditTarget] = useState<ServiceConfig | null>(null)
  const [createOpen, setCreateOpen] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<string | null>(null)

  const { data, isLoading } = useQuery({
    queryKey: ['config-services'],
    queryFn: fetchConfigServices,
  })

  const createMut = useMutation({
    mutationFn: createConfigService,
    onSuccess: () => {
      toast.success('Service created')
      setCreateOpen(false)
      void qc.invalidateQueries({ queryKey: ['config-services'] })
    },
    onError: (e: Error) => toast.error(e.message),
  })

  const updateMut = useMutation({
    mutationFn: (payload: { name: string; data: ServiceConfigInput }) =>
      updateConfigService(payload.name, payload.data),
    onSuccess: () => {
      toast.success('Service updated')
      setEditTarget(null)
      void qc.invalidateQueries({ queryKey: ['config-services'] })
    },
    onError: (e: Error) => toast.error(e.message),
  })

  const deleteMut = useMutation({
    mutationFn: deleteConfigService,
    onSuccess: () => {
      toast.success('Service deleted')
      setDeleteTarget(null)
      void qc.invalidateQueries({ queryKey: ['config-services'] })
    },
    onError: (e: Error) => toast.error(e.message),
  })

  const services = data?.services ?? []

  return (
    <div className="page">
      <div className="page-header">
        <div>
          <p className="eyebrow flex items-center gap-2"><Settings2 className="size-4" /> Service registry</p>
          <h1 className="page-title">Config</h1>
          <p className="page-subtitle">Manage monitored endpoints, dependencies, tags, and recovery actions.</p>
        </div>
        <button
          onClick={() => setCreateOpen(true)}
          className="action-button action-primary"
        >
          <Plus className="size-4" /> Add Service
        </button>
      </div>

      {isLoading && (
        <div className="flex justify-center py-20">
          <Spinner size="lg" />
        </div>
      )}

      {!isLoading && services.length === 0 && (
        <EmptyState
          title="No services configured"
          description="Add a service to start monitoring."
          action={
            <button
              onClick={() => setCreateOpen(true)}
              className="action-button action-primary"
            >
              Add first service
            </button>
          }
        />
      )}

      {services.length > 0 && (
        <div className="panel overflow-hidden">
          <table className="data-table">
            <thead>
              <tr>
                <th className="px-4 py-3 text-left">Name</th>
                <th className="px-4 py-3 text-left">URL</th>
                <th className="px-4 py-3 text-left">Interval</th>
                <th className="px-4 py-3 text-left">Tags</th>
                <th className="px-4 py-3 text-left">Dependencies</th>
                <th className="px-4 py-3 text-left">Healing</th>
                <th className="px-4 py-3" />
              </tr>
            </thead>
            <tbody>
              {services.map((svc) => (
                <tr
                  key={svc.name}
                >
                  <td className="px-4 py-3 font-semibold" style={{ color: 'var(--text-1)' }}>{svc.name}</td>
                  <td className="max-w-[200px] px-4 py-3">
                    <span className="block truncate" style={{ color: 'var(--text-3)' }} title={svc.url}>
                      {svc.url}
                    </span>
                  </td>
                  <td className="px-4 py-3" style={{ color: 'var(--text-2)' }}>{formatNsDuration(svc.interval)}</td>
                  <td className="px-4 py-3">
                    <div className="flex flex-wrap gap-1">
                      {(svc.tags ?? []).map((t) => (
                        <span
                          key={t}
                          className="tag"
                        >
                          {t}
                        </span>
                      ))}
                    </div>
                  </td>
                  <td className="px-4 py-3 text-xs" style={{ color: 'var(--text-3)' }}>
                    {(svc.dependencies ?? []).join(', ') || '—'}
                  </td>
                  <td className="px-4 py-3 text-xs" style={{ color: 'var(--text-3)' }}>
                    {(svc.healing_actions ?? []).join(', ') || '—'}
                  </td>
                  <td className="px-4 py-3">
                    <div className="flex items-center justify-end gap-2">
                      <button
                        onClick={() => setEditTarget(svc)}
                        className="icon-button size-9 shadow-none"
                        title="Edit"
                      >
                        <Pencil className="size-3.5" />
                      </button>
                      <button
                        onClick={() => setDeleteTarget(svc.name)}
                        className="icon-button size-9 shadow-none hover:text-red-600"
                        title="Delete"
                      >
                        <Trash2 className="size-3.5" />
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {/* Create dialog */}
      <Dialog.Root open={createOpen} onOpenChange={setCreateOpen}>
        <Dialog.Portal>
          <Dialog.Overlay className="fixed inset-0 bg-black/30 backdrop-blur-sm" />
          <Dialog.Content
            className="panel panel-strong fixed left-1/2 top-1/2 w-full max-w-lg -translate-x-1/2 -translate-y-1/2 p-6"
          >
            <div className="mb-4 flex items-center justify-between">
              <Dialog.Title className="flex items-center gap-2 text-lg font-black" style={{ color: 'var(--text-1)' }}>
                <Wrench className="size-5" style={{ color: 'var(--accent)' }} />
                Add Service
              </Dialog.Title>
              <Dialog.Close className="icon-button size-9 shadow-none">
                <X className="size-4" />
              </Dialog.Close>
            </div>
            <ServiceForm
              initial={EMPTY_FORM}
              onSave={(v) => createMut.mutate(v)}
              saving={createMut.isPending}
            />
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>

      {/* Edit dialog */}
      <Dialog.Root open={!!editTarget} onOpenChange={(o) => !o && setEditTarget(null)}>
        <Dialog.Portal>
          <Dialog.Overlay className="fixed inset-0 bg-black/30 backdrop-blur-sm" />
          <Dialog.Content
            className="panel panel-strong fixed left-1/2 top-1/2 w-full max-w-lg -translate-x-1/2 -translate-y-1/2 p-6"
          >
            <div className="mb-4 flex items-center justify-between">
              <Dialog.Title className="flex items-center gap-2 text-lg font-black" style={{ color: 'var(--text-1)' }}>
                <Wrench className="size-5" style={{ color: 'var(--accent)' }} />
                Edit {editTarget?.name}
              </Dialog.Title>
              <Dialog.Close className="icon-button size-9 shadow-none">
                <X className="size-4" />
              </Dialog.Close>
            </div>
            {editTarget && (
              <ServiceForm
                initial={{
                  name: editTarget.name,
                  url: editTarget.url,
                  interval: formatNsDuration(editTarget.interval),
                  timeout: formatNsDuration(editTarget.timeout),
                  tags: editTarget.tags,
                  dependencies: editTarget.dependencies,
                  healing_actions: editTarget.healing_actions,
                  container_name: editTarget.container_name,
                  fallback_url: editTarget.fallback_url,
                  healing_webhook: editTarget.healing_webhook,
                }}
                onSave={(v) =>
                  updateMut.mutate({ name: editTarget.name, data: v })
                }
                saving={updateMut.isPending}
              />
            )}
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>

      {/* Delete confirm */}
      <Dialog.Root
        open={!!deleteTarget}
        onOpenChange={(o) => !o && setDeleteTarget(null)}
      >
        <Dialog.Portal>
          <Dialog.Overlay className="fixed inset-0 bg-black/30 backdrop-blur-sm" />
          <Dialog.Content
            className="panel panel-strong fixed left-1/2 top-1/2 w-96 -translate-x-1/2 -translate-y-1/2 p-6"
          >
            <Dialog.Title className="mb-2 text-lg font-black" style={{ color: 'var(--text-1)' }}>
              Delete service
            </Dialog.Title>
            <p className="mb-6 text-sm" style={{ color: 'var(--text-2)' }}>
              Are you sure you want to delete <span className="font-semibold" style={{ color: 'var(--text-1)' }}>{deleteTarget}</span>?
              This will remove it from the config and stop monitoring.
            </p>
            <div className="flex justify-end gap-3">
              <Dialog.Close
                className="action-button"
              >
                Cancel
              </Dialog.Close>
              <button
                onClick={() => deleteTarget && deleteMut.mutate(deleteTarget)}
                disabled={deleteMut.isPending}
                className="action-button action-danger disabled:opacity-40"
              >
                {deleteMut.isPending ? 'Deleting…' : 'Delete'}
              </button>
            </div>
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>
    </div>
  )
}
