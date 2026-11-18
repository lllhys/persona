//! Pairing over USB (`usb-list`, `pair`) and matching a Bonjour advertisement to a record (`match`), #3448.
//!
//! `pair` reaches the phone's `remotepairingdeviced` through the lockdown service over usbmuxd, which needs the phone
//! to trust this computer already (the "Trust This Computer?" prompt, answered once). It then runs RemotePairing's
//! manual pairing: the phone asks its user to allow it and for the passcode, and hands back the key (`altIRK`) its
//! Bonjour advertisements are tagged with. The record (`record::Record`) keeps both. `usb-list` is checked on a real
//! iPhone; `pair` is not verified on a real device yet.

use std::path::Path;

use idevice::IdeviceService as _;
use idevice::amfi::AmfiClient;
use idevice::lockdown::LockdownClient;
use idevice::provider::{IdeviceProvider as _, UsbmuxdProvider};
use idevice::remote_pairing::{PeerDevice, RemotePairingLockdownService};
use idevice::usbmuxd::{Connection, UsbmuxdAddr, UsbmuxdConnection, UsbmuxdDevice};
use serde_json::{Value, json};

use crate::error::{BridgeError, Code, Phase, from_idevice};
use crate::record::{Record, Stored};

const LABEL: &str = "persona-iphone-bridge";

/// The PIN of an iPhone's manual RemotePairing when it shows none: always 000000.
const PAIRING_PIN: &str = "000000";

/// Phones attached over USB: `[{"udid", "name", "product_version", "model"}]`.
pub async fn usb_list() -> Result<Value, BridgeError> {
    let mut out = Vec::new();
    for device in usb_devices().await? {
        let facts = Facts::read(&provider(&device)).await;
        out.push(json!({
            "udid": device.udid,
            "name": facts.name,
            "product_version": facts.product_version,
            "model": facts.product_type,
        }));
    }
    Ok(Value::Array(out))
}

/// Pairs with the phone `udid` over trusted USB and writes the record to `out`:
/// `{"udid", "name", "model", "product_version", "developer_mode"}`. Pairing works with Developer Mode off, but the
/// phone then neither advertises nor takes connections over the network, so `developer_mode` says so up front
/// (`null` when the phone didn't say).
pub async fn pair(udid: &str, out: &Path) -> Result<Value, BridgeError> {
    let device = usb_devices()
        .await?
        .into_iter()
        .find(|d| d.udid == udid)
        .ok_or_else(|| BridgeError::new(Code::NotFound, format!("no iPhone {udid} is attached over USB")))?;
    let provider = provider(&device);
    if provider.get_pairing_file().await.is_err() {
        return Err(BridgeError::new(
            Code::TrustPending,
            "the phone doesn't trust this computer yet: unlock it, tap Trust and enter its passcode, then try again",
        ));
    }
    let facts = Facts::read(&provider).await;

    let service = RemotePairingLockdownService::connect(&provider)
        .await
        .map_err(|e| from_idevice(e, Phase::Pairing, "open the pairing service"))?;
    let mut record = Record::generate();
    let mut client = service
        .into_client(&record.sending_host)
        .map_err(|e| from_idevice(e, Phase::Pairing, "start pairing"))?;
    // The PIN for the pairing's SRP exchange. The library uses 000000 itself when the phone asks its user to allow
    // the pairing, and asks this callback when the phone answers with pairing data straight away. An iPhone that
    // already trusts this computer does the latter (seen on a real iPhone, iOS 18.7.3), and its manual pairing uses
    // the same fixed PIN; an empty one made the library panic slicing it.
    client
        .connect(&mut record.pairing, || async { PAIRING_PIN.to_owned() })
        .await
        .map_err(|e| from_idevice(e, Phase::Pairing, "pair"))?;
    if record.pairing.alt_irk().is_none() {
        return Err(BridgeError::internal(
            "the phone paired but gave no key for its Bonjour advertisements",
        ));
    }
    let peer = client.paired_peer_device().ok();
    record.write(out)?;
    Ok(json!({
        "udid": udid,
        "name": peer.map(|p| p.name.clone()).or(facts.name),
        "model": peer.map(|p| p.model.clone()).or(facts.product_type),
        "product_version": facts.product_version,
        "developer_mode": facts.developer_mode,
    }))
}

/// Shows Developer Mode in the phone's Settings › Privacy & Security (`reveal-developer-mode`, #8199), where iOS keeps
/// it hidden until a developer tool asks for it: through the AMFI lockdown service over usbmuxd, so the phone has to be
/// on the cable and trust this computer. It turns nothing on: the phone's user still turns it on, and the phone
/// restarts. `{"udid", "revealed": true}`. Not verified on a real device.
pub async fn reveal_developer_mode(udid: &str) -> Result<Value, BridgeError> {
    let device = usb_devices()
        .await?
        .into_iter()
        .find(|d| d.udid == udid)
        .ok_or_else(|| BridgeError::new(Code::NotFound, format!("no iPhone {udid} is attached over USB")))?;
    let provider = provider(&device);
    if provider.get_pairing_file().await.is_err() {
        return Err(BridgeError::new(
            Code::TrustPending,
            "the phone doesn't trust this computer yet: unlock it, tap Trust and enter its passcode, then try again",
        ));
    }
    let mut amfi = AmfiClient::connect(&provider)
        .await
        .map_err(|e| from_idevice(e, Phase::Pairing, "open the AMFI service"))?;
    amfi.reveal_developer_mode_option_in_ui()
        .await
        .map_err(|e| from_idevice(e, Phase::Pairing, "show Developer Mode in Settings"))?;
    Ok(json!({ "udid": udid, "revealed": true }))
}

/// Whether a `_remotepairing._tcp` advertisement (its TXT `identifier` and `authTag`) is the phone of the
/// record at `record`: `{"match": true|false}`.
pub async fn match_record(record: &Path, identifier: &str, auth_tag: &str) -> Result<Value, BridgeError> {
    match Stored::read(record)? {
        Stored::Network(record) => Ok(json!({ "match": matches(&record, identifier, auth_tag)? })),
        // The node never browses for one; a simulator has no advertisement to check.
        Stored::Simulator(_) => Err(BridgeError::bad_request(
            "a simulator's record has no Bonjour advertisement to match",
        )),
    }
}

fn matches(record: &Record, identifier: &str, auth_tag: &str) -> Result<bool, BridgeError> {
    let irk = record.pairing.alt_irk().ok_or_else(|| {
        BridgeError::new(
            Code::PairingMissing,
            "the pairing record has no Bonjour key: pair the phone again",
        )
    })?;
    Ok(PeerDevice::validate_auth_tag(irk, identifier, auth_tag))
}

/// The phones usbmuxd sees over USB (not over Wi-Fi), each once.
async fn usb_devices() -> Result<Vec<UsbmuxdDevice>, BridgeError> {
    let unavailable = |e| BridgeError::new(Code::ServiceUnavailable, format!("usbmuxd: {e}"));
    let mut mux = UsbmuxdConnection::default().await.map_err(unavailable)?;
    Ok(usb_only(mux.get_devices().await.map_err(unavailable)?))
}

fn usb_only(devices: Vec<UsbmuxdDevice>) -> Vec<UsbmuxdDevice> {
    let mut out: Vec<UsbmuxdDevice> = Vec::new();
    for d in devices {
        if d.connection_type == Connection::Usb && !out.iter().any(|o| o.udid == d.udid) {
            out.push(d);
        }
    }
    out
}

fn provider(device: &UsbmuxdDevice) -> UsbmuxdProvider {
    device.to_provider(UsbmuxdAddr::default(), LABEL)
}

/// What lockdown says about a phone, best effort: the model and iOS version need no session; recent iOS gives
/// its real name only in one, which needs this computer to be trusted.
#[derive(Default)]
struct Facts {
    name: Option<String>,
    product_type: Option<String>,
    product_version: Option<String>,
    developer_mode: Option<bool>,
}

impl Facts {
    async fn read(provider: &UsbmuxdProvider) -> Self {
        let Ok(mut lockdown) = LockdownClient::connect(provider).await else {
            return Self::default();
        };
        let mut facts = Self {
            product_type: value(&mut lockdown, "ProductType").await,
            product_version: value(&mut lockdown, "ProductVersion").await,
            name: None,
            developer_mode: None,
        };
        if let Ok(trust) = provider.get_pairing_file().await
            && lockdown.start_session(&trust).await.is_ok()
        {
            facts.name = value(&mut lockdown, "DeviceName").await;
            facts.developer_mode = lockdown
                .get_value(Some("DeveloperModeStatus"), Some("com.apple.security.mac.amfi"))
                .await
                .ok()
                .and_then(|v| v.as_boolean());
        }
        facts
    }
}

async fn value(lockdown: &mut LockdownClient, key: &str) -> Option<String> {
    lockdown
        .get_value(Some(key), None)
        .await
        .ok()
        .and_then(|v| v.as_string().map(str::to_owned))
}

#[cfg(test)]
mod tests {
    use base64::Engine as _;
    use idevice::remote_pairing::compute_auth_tag;

    use super::*;

    fn device(udid: &str, connection_type: Connection) -> UsbmuxdDevice {
        UsbmuxdDevice {
            connection_type,
            udid: udid.into(),
            device_id: 1,
        }
    }

    #[test]
    fn usb_list_keeps_usb_phones_once() {
        let net = Connection::Network(std::net::IpAddr::from([192, 0, 2, 1]));
        let got = usb_only(vec![
            device("A", Connection::Usb),
            device("B", net),
            device("A", Connection::Usb),
            device("C", Connection::Usb),
        ]);
        let udids: Vec<_> = got.iter().map(|d| d.udid.as_str()).collect();
        assert_eq!(udids, ["A", "C"]);
    }

    #[test]
    fn matches_advertisements_by_the_records_key() {
        let mut record = Record::generate();
        let irk = [7u8; 16];
        record.pairing.alt_irk = Some(irk.to_vec());
        let tag = |id: &str| base64::engine::general_purpose::STANDARD.encode(compute_auth_tag(&irk, id));

        assert!(matches(&record, "id-1", &tag("id-1")).unwrap());
        assert!(!matches(&record, "id-2", &tag("id-1")).unwrap(), "another identifier");
        assert!(!matches(&record, "id-1", "not base64!").unwrap());

        // The key survives the record file.
        let back = Record::from_bytes(&record.to_bytes()).unwrap();
        assert!(matches(&back, "id-3", &tag("id-3")).unwrap());

        let simulator = std::env::temp_dir().join(format!("persona-iphone-bridge-sim-{}.record", std::process::id()));
        crate::record::SimRecord { udid: "SIM-1".into() }
            .write(&simulator)
            .unwrap();
        let rt = tokio::runtime::Builder::new_current_thread().build().unwrap();
        let err = rt.block_on(match_record(&simulator, "id-1", &tag("id-1"))).unwrap_err();
        assert_eq!(err.code, Code::BadRequest, "a simulator has nothing to match");
        let network = std::env::temp_dir().join(format!("persona-iphone-bridge-net-{}.record", std::process::id()));
        record.write(&network).unwrap();
        let answer = rt.block_on(match_record(&network, "id-4", &tag("id-4"))).unwrap();
        assert_eq!(answer, json!({"match": true}));
        std::fs::remove_file(&simulator).unwrap();
        std::fs::remove_file(&network).unwrap();

        let unpaired = Record::generate();
        assert_eq!(
            matches(&unpaired, "id-1", &tag("id-1")).unwrap_err().code,
            Code::PairingMissing
        );
    }
}
