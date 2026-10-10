// TypeScript mirror of the JSON the Go API sends.
// The Go side converts protobuf with protojson: camelCase keys, enums as their string names,
// zero values included, unset sub-messages as null. Heads-up: int64 fields (timestampMs) arrive as STRINGS.

export type VehicleModel =
  | 'VEHICLE_MODEL_UNSPECIFIED'
  | 'VEHICLE_MODEL_TESLA_MODEL_3'
  | 'VEHICLE_MODEL_TESLA_MODEL_Y'
  | 'VEHICLE_MODEL_TESLA_MODEL_S'
  | 'VEHICLE_MODEL_TESLA_MODEL_X'
  | 'VEHICLE_MODEL_TESLA_CYBERTRUCK'
  | 'VEHICLE_MODEL_TESLA_SEMI'
  | 'VEHICLE_MODEL_RIVIAN_R1T'
  | 'VEHICLE_MODEL_RIVIAN_R1S'
  | 'VEHICLE_MODEL_RIVIAN_EDV'

export type Gear = 'GEAR_UNSPECIFIED' | 'GEAR_PARK' | 'GEAR_REVERSE' | 'GEAR_NEUTRAL' | 'GEAR_DRIVE'

export interface GPSLocation {
  latitude: number
  longitude: number
  speed: number
  heading: number
}

export interface BatteryState {
  stateOfCharge: number
  packVoltage: number
  packPower: number
  packTemperature: number
  maxCellTemperature: number
  minCellVoltage: number
  maxCellVoltage: number
  lowVoltageBatteryVolts: number
}

export interface ChargingState {
  state: string // CHARGE_STATE_CHARGING, ...
  chargerType: string
  chargingPowerKw: number
  minutesToFullCharge: number
  chargeLimitSocPct: number
}

export interface TirePressure {
  frontLeftBar: number
  frontRightBar: number
  rearLeftBar: number
  rearRightBar: number
}

export interface CabinState {
  insideTempCelsius: number
  outsideTempCelsius: number
  climateOn: boolean
  isLocked: boolean
  frunkOpen: boolean
  trunkOrLiftgateOpen: boolean
  driverDoorOpen: boolean
}

export interface TruckState {
  tailgateOpen: boolean
  towModeActive: boolean
  trailerConnected: boolean
  estimatedTrailerWeightKg: number
  bedOutletsActive: boolean
  bedOutletsPowerKw: number
}

export interface VehicleTelemetry {
  vin: string
  timestampMs: string
  location: GPSLocation | null
  model: VehicleModel
  gear: Gear
  speedKmh: number
  odometerKm: number
  remainingRangeKm: number
  vehicleMode: string // simulator scenario, e.g. HIGHWAY_CRUISING
  batteryState: BatteryState | null
  chargingState: ChargingState | null
  tirePressure: TirePressure | null
  cabinState: CabinState | null
  truckState: TruckState | null
  activeAlertCodes: string[]
}

/** One entry from GET /api/v1/vehicles and every WebSocket push */
export interface Vehicle {
  vin: string
  online: boolean
  telemetry: VehicleTelemetry
}

export type AlertLevel =
  | 'ALERT_LEVEL_UNSPECIFIED'
  | 'ALERT_LEVEL_INFO'
  | 'ALERT_LEVEL_WARNING'
  | 'ALERT_LEVEL_ERROR'
  | 'ALERT_LEVEL_CRITICAL'

export interface VehicleAlert {
  vin: string
  timestampMs: string
  level: AlertLevel
  code: string
  description: string
}

export type CommandType =
  | 'UNLOCK_DOORS'
  | 'LOCK_DOORS'
  | 'OPEN_FRUNK'
  | 'CLOSE_FRUNK'
  | 'OPEN_TRUNK'
  | 'CLOSE_TRUNK'
  | 'SET_SPEED_LIMIT'
  | 'REBOOT_COMPUTER'

/** POST /command response. success=false means the VEHICLE refused (safety interlock / signature) */
export interface CommandResult {
  command_id: string
  success: boolean
  error?: string
}
