import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { api } from '../../lib/api'
import { apiErrorMessage } from '../../lib/api/errorMessage'
import { attachHidScanner, createScanner, decodeImageFile, describeScannerSupport, normalizeScanValue, type Scanner, type ScannerError, type ScannerErrorCode, type ScannerStatus, type ZoomRange } from '../../lib/scanner'
import { Alert, Button, Card, Field, Input, Notice as NoticeMessage, PageHeader } from '../../components/ui'
import { useRentals } from '../rentals/RentalContext'
const CAMERA_ERROR_KEYS: Record<ScannerErrorCode,string> = { insecure_context:'scan.insecureContext', permission_denied:'scan.permissionDenied', no_camera:'scan.noCamera', camera_in_use:'scan.cameraInUse', unsupported:'scan.unsupported', unknown:'errors.server' }
export function ScanView() {
 const {t}=useTranslation(); const rentals=useRentals(); const rentalsRef=useRef(rentals);rentalsRef.current=rentals
 const pending=useRef(new Set<string>())
 const [notice,setNotice]=useState<{kind:'success'|'info'|'error';message:string}|null>(null)
 const [manualCode,setManualCode]=useState('')
 const [cameraError,setCameraError]=useState<ScannerError|null>(null)
 const [cameraStatus,setCameraStatus]=useState<ScannerStatus>('idle')
 const [torchOn,setTorchOn]=useState(false);const [torchSupported,setTorchSupported]=useState(false)
 const [zoomRange,setZoomRange]=useState<ZoomRange|null>(null);const [zoom,setZoomLevel]=useState(1)
 const [photoBusy,setPhotoBusy]=useState(false);const photoRef=useRef<HTMLInputElement|null>(null)
 const videoRef=useRef<HTMLVideoElement|null>(null);const scannerRef=useRef<Scanner|null>(null)
 const support=useMemo(()=>describeScannerSupport(),[])
 const addCode=useCallback(async(raw:string)=>{
  const codes=raw.split(/[,;\n]+/).map(normalizeScanValue).filter(Boolean)
  const fresh=codes.filter(code=>!pending.current.has(code)&&!rentalsRef.current.items.some(i=>i.asset_tag.toUpperCase()===code))
  if(!fresh.length){if(codes.length)setNotice({kind:'info',message:t('scan.duplicate')});return}
  fresh.forEach(code=>pending.current.add(code))
  try {const result=await api.items.resolve(fresh);rentalsRef.current.add(result.matched);setNotice({kind:result.unknown.length?'info':'success',message:result.unknown.length?`${t('scan.unknown')}: ${result.unknown.join(', ')}`:`${t('scan.matched')}: ${result.matched.map(i=>i.asset_tag).join(', ')}`})}
  catch(error){setNotice({kind:'error',message:apiErrorMessage(error,t)})}
  finally{fresh.forEach(code=>pending.current.delete(code))}
 },[t])
 const addCodeRef=useRef(addCode);addCodeRef.current=addCode
  // ------------------------------------------------------------------ camera
  async function startCamera(): Promise<void> {
    const video = videoRef.current
    if (!video) return
    setCameraError(null)
    if (!scannerRef.current) {
      scannerRef.current = createScanner({
        video,
        onResult: (result) => void addCodeRef.current(result.value),
        onError: (error) => setCameraError(error),
        onStatus: (status) => setCameraStatus(status),
        dedupeWindowMs: 1500,
        feedback: true,
      })
    }
    await scannerRef.current.start()
    setTorchSupported(scannerRef.current.torchSupported)
    const range = scannerRef.current.zoomRange
    setZoomRange(range)
    // Start at 2x where the camera allows it: the label fills more of the
    // frame while the phone stays far enough away to focus.
    if (range && range.max >= 2 && (await scannerRef.current.setZoom(2))) setZoomLevel(2)
    else setZoomLevel(range?.min ?? 1)
  }

  async function changeZoom(level: number): Promise<void> {
    if (await scannerRef.current?.setZoom(level)) setZoomLevel(level)
  }

  async function scanPhoto(file: File | undefined): Promise<void> {
    if (!file) return
    setPhotoBusy(true)
    try {
      const value = await decodeImageFile(file)
      if (value) await addCodeRef.current(value)
      else setNotice({ kind: 'error', message: t('scan.photoNotFound') })
    } catch {
      setNotice({ kind: 'error', message: t('scan.photoNotFound') })
    } finally {
      setPhotoBusy(false)
      if (photoRef.current) photoRef.current.value = ''
    }
  }

  function stopCamera(): void {
    scannerRef.current?.stop()
    setZoomRange(null)
    setTorchSupported(false)
    setTorchOn(false)
  }

  async function toggleTorch(): Promise<void> {
    const scanner = scannerRef.current
    if (!scanner) return
    const next = !torchOn
    if (await scanner.toggleTorch(next)) setTorchOn(next)
  }

  // Stop the camera and detach the HID listener when leaving the page.
  useEffect(() => {
    return () => {
      scannerRef.current?.stop()
      scannerRef.current = null
    }
  }, [])

  useEffect(
    () =>
      attachHidScanner({
        onScan: (code) => void addCodeRef.current(code),
        target: document,
        minLength: 3,
      }),
    [],
  )

 const isScanning=cameraStatus==='scanning'||cameraStatus==='starting'
 return <div className="space-y-4"><PageHeader title={t('scan.title')} description={t('rental.scanHelp')}/>
 {notice && <NoticeMessage variant={notice.kind} onDismiss={()=>setNotice(null)} dismissLabel={t('common.close')}>{notice.message}</NoticeMessage>}
      <div className="grid gap-4 lg:grid-cols-2">
        {/* Camera -------------------------------------------------------- */}
        <Card className="space-y-3 p-4">
          {!support.secureContext ? (
            <Alert variant="warning">{t('scan.insecureContext')}</Alert>
          ) : !support.mediaDevices ? (
            <Alert variant="warning">{t('scan.unsupported')}</Alert>
          ) : null}

          {cameraError ? (
            <Alert variant="error">{t(CAMERA_ERROR_KEYS[cameraError.code])}</Alert>
          ) : null}

          <div className={isScanning ? "relative overflow-hidden rounded-lg bg-content" : "hidden"}>
            <video
              ref={videoRef}
              className="aspect-video w-full bg-content object-cover"
              playsInline
              muted
            />
            {/* Aiming line: hold the label across it, about 15-25 cm from the lens. */}
            <span aria-hidden="true" className="pointer-events-none absolute inset-x-[8%] top-1/2 h-0.5 -translate-y-1/2 rounded-full bg-[#78be5a]/80 shadow-[0_0_6px_rgba(120,190,90,.8)]" />
            {cameraStatus === 'scanning' ? (
              <span className="absolute left-3 top-3 rounded-full bg-content/90 px-2 py-0.5 text-xs text-content-inverse">
                {t('scan.scanning')}
              </span>
            ) : null}
          </div>

          {isScanning && (zoomRange || torchSupported) ? (
            <div className="flex flex-wrap items-center gap-2">
              {zoomRange ? (
                <div className="flex flex-wrap items-center gap-2" role="group" aria-label={t('scan.zoom')}>
                  <span className="text-xs text-content-muted">{t('scan.zoom')}</span>
                  {[1, 2, 3].filter((level) => level >= zoomRange.min && level <= zoomRange.max).map((level) => (
                    <Button key={level} size="sm" variant={zoom === level ? 'primary' : 'secondary'} aria-pressed={zoom === level} onClick={() => void changeZoom(level)}>
                      {level}×
                    </Button>
                  ))}
                </div>
              ) : null}
              {torchSupported ? (
                <Button size="sm" variant={torchOn ? 'primary' : 'secondary'} aria-pressed={torchOn} className={zoomRange ? 'ml-auto' : undefined} onClick={() => void toggleTorch()}>
                  {torchOn ? t('scan.torchOff') : t('scan.torchOn')}
                </Button>
              ) : null}
            </div>
          ) : null}

          <p className="text-xs text-content-muted">{t('scan.hint')}</p>

          <div className="flex flex-wrap items-center gap-2">
            {isScanning ? (
              <Button variant="danger" size="sm" onClick={stopCamera}>
                {t('scan.stop')}
              </Button>
            ) : (
              <Button
                variant="primary"
                size="sm"
                disabled={!support.secureContext || !support.mediaDevices}
                onClick={() => void startCamera()}
              >
                {t('scan.start')}
              </Button>
            )}
            <Button variant="secondary" size="sm" disabled={photoBusy} onClick={() => photoRef.current?.click()}>
              {photoBusy ? t('scan.photoReading') : t('scan.photo')}
            </Button>
            <input
              ref={photoRef}
              type="file"
              accept="image/*"
              capture="environment"
              className="hidden"
              aria-hidden="true"
              tabIndex={-1}
              onChange={(event) => void scanPhoto(event.target.files?.[0])}
            />
          </div>
          <p className="text-xs text-content-muted">{t('scan.photoHint')}</p>
        </Card>

        {/* Manual / HID entry ------------------------------------------- */}
        <Card className="space-y-3 p-4">
          <Field label={t('scan.manualEntry')}>
            {({ id, 'aria-describedby': describedBy }) => (
              <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
                <Input
                  id={id}
                  aria-describedby={describedBy}
                  value={manualCode}
                  placeholder={t('scan.manualPlaceholder')}
                  onChange={(event) => setManualCode(event.target.value)}
                  onKeyDown={(event) => {
                    if (event.key === 'Enter') {
                      event.preventDefault()
                      void addCode(manualCode)
                      setManualCode('')
                    }
                  }}
                />
                <Button
                  variant="secondary"
                  className="shrink-0 whitespace-nowrap"
                  disabled={!manualCode.trim()}
                  onClick={() => {
                    void addCode(manualCode)
                    setManualCode('')
                  }}
                >
                  {t('scan.addToBuffer')}
                </Button>
              </div>
            )}
          </Field>
          <p className="text-xs text-content-muted">{t('scan.hidHint')}</p>
        </Card>
      </div>

</div>
}
