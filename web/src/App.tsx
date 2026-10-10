import { useEffect, useMemo, useState } from 'react'
import { Check, X } from 'lucide-react'
import { AlertsPanel } from './components/AlertsPanel'
import { FleetList } from './components/FleetList'
import { FleetMap } from './components/FleetMap'
import { PanelBoundary } from './components/PanelBoundary'
import { TopBar } from './components/TopBar'
import { VehicleDetail } from './components/VehicleDetail'
import { mergeVehicleDtcs, useAlerts } from './hooks/useAlerts'
import { useCommands } from './hooks/useCommands'
import { useFleet } from './hooks/useFleet'
import { useTheme } from './hooks/useTheme'
import { CREDITS } from './lib/models'

/** Selected VIN lives in the URL hash (#SIM-0004) so a view can be bookmarked or shared */
function useSelectedVin(): [string | null, (vin: string | null) => void] {
  const read = () => decodeURIComponent(window.location.hash.slice(1)) || null
  const [vin, setVin] = useState<string | null>(read)
  useEffect(() => {
    const onHash = () => setVin(read())
    window.addEventListener('hashchange', onHash)
    return () => window.removeEventListener('hashchange', onHash)
  }, [])
  const select = (next: string | null) => {
    window.location.hash = next ? encodeURIComponent(next) : ''
    setVin(next)
  }
  return [vin, select]
}

export default function App() {
  const fleet = useFleet()
  const [theme, toggleTheme] = useTheme()
  const { alerts: ruleAlerts } = useAlerts()
  const commands = useCommands()
  const [selectedVin, select] = useSelectedVin()

  const vehicles = useMemo(() => [...fleet.vehicles.values()], [fleet.vehicles])
  const alerts = useMemo(() => mergeVehicleDtcs(ruleAlerts, vehicles), [ruleAlerts, vehicles])
  const alertVins = useMemo(() => new Set(alerts.map((a) => a.vin)), [alerts])
  const selected = selectedVin ? fleet.vehicles.get(selectedVin) : undefined

  // Esc closes the vehicle panel
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && select(null)
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  })

  return (
    <div className={`app ${selected ? 'has-detail' : ''}`}>
      <TopBar vehicles={vehicles} alertVins={alertVins} alertCount={alerts.length} link={fleet.status} theme={theme} onToggleTheme={toggleTheme} />

      <FleetList vehicles={vehicles} alertVins={alertVins} selected={selectedVin} onSelect={select} />

      <main className="center">
        <PanelBoundary name="Map">
          <FleetMap vehicles={vehicles} alertVins={alertVins} selected={selectedVin} onSelect={select} theme={theme} />
        </PanelBoundary>
        <AlertsPanel alerts={alerts} onSelect={select} />
      </main>

      {selected && (
        <VehicleDetail
          vehicle={selected}
          alertVins={alertVins}
          pending={commands.pendingFor(selected.vin)}
          theme={theme}
          onCommand={(cmd, mph) => commands.run(selected.vin, cmd, mph)}
          onClose={() => select(null)}
        />
      )}

      <div className="toasts" aria-live="assertive">
        {commands.toasts.map((t) => (
          <div key={t.id} className={`toast ${t.ok ? 'toast-ok' : 'toast-fail'}`} role="status">
            <span className="toast-icon" aria-hidden>{t.ok ? <Check size={16} strokeWidth={3} /> : <X size={16} strokeWidth={3} />}</span>
            <div className="toast-body">
              <strong>{t.command.replace(/_/g, ' ').toLowerCase()} · {t.vin}</strong>
              <p>{t.message}</p>
            </div>
            <button className="toast-close" onClick={() => commands.dismiss(t.id)} aria-label="Dismiss"><X size={14} /></button>
          </div>
        ))}
      </div>

      <footer className="credits">
        3D models:{' '}
        {CREDITS.map((c) => (
          <span key={c.title}>
            <a href={c.sourceUrl} target="_blank" rel="noreferrer">{c.title}</a> by {c.author} (
            <a href={c.licenseUrl} target="_blank" rel="noreferrer">{c.license}</a>)
          </span>
        ))}
      </footer>
    </div>
  )
}
