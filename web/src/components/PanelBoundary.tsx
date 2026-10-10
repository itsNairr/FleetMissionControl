import { Component, type ReactNode } from 'react'

interface Props {
  name: string
  children: ReactNode
}

/** Contains a crash to one panel (e.g. WebGL unavailable) instead of blanking the whole dashboard */
export class PanelBoundary extends Component<Props, { error: Error | null }> {
  state = { error: null as Error | null }

  static getDerivedStateFromError(error: Error) {
    return { error }
  }

  render() {
    if (this.state.error) {
      return (
        <div className="panel-error" role="alert">
          <strong>{this.props.name} failed to render</strong>
          <p>{this.state.error.message}</p>
          <button className="btn btn-light" onClick={() => this.setState({ error: null })}>Retry</button>
        </div>
      )
    }
    return this.props.children
  }
}
