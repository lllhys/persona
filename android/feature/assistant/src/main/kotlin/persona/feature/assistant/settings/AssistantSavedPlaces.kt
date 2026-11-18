package persona.feature.assistant.settings

import android.location.Geocoder
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalResources
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.material3.Text
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import persona.core.contracts.api.SavedPlace
import persona.core.designsystem.GroupPosition
import persona.core.designsystem.JButton
import persona.core.designsystem.JButtonKind
import persona.core.designsystem.JDialog
import persona.core.designsystem.JEmptyState
import persona.core.designsystem.JGroupItem
import persona.core.designsystem.JIconButton
import persona.core.designsystem.JListPage
import persona.core.designsystem.JLoading
import persona.core.designsystem.JNotice
import persona.core.designsystem.JRadio
import persona.core.designsystem.JRow
import persona.core.designsystem.JSectionHeader
import persona.core.designsystem.JTextField
import persona.core.designsystem.PersonaTheme
import persona.core.designsystem.Status
import persona.core.designsystem.Tokens
import persona.core.designsystem.currentLocale
import persona.core.designsystem.icons.PersonaIcons
import persona.core.designsystem.pagePadding
import persona.feature.assistant.R

/** The Saved Places list (Settings › Assistant › Saved Places, #9293). */
@Composable
internal fun PlacesPart(
    state: AssistantSettingsUiState,
    onBack: () -> Unit,
    onRefresh: () -> Unit,
    onOpen: (String?) -> Unit,
    onDelete: suspend (SavedPlace) -> Unit,
    previewDelete: Boolean = false,
) {
    val places = state.places
    val entries = places.entries.orEmpty()
    val locale = currentLocale
    var pendingDelete by remember { mutableStateOf<SavedPlace?>(null) }
    val scope = rememberCoroutineScope()
    LaunchedEffect(previewDelete, entries) {
        if (previewDelete && pendingDelete == null) pendingDelete = entries.firstOrNull()
    }
    JListPage(
        title = stringResource(R.string.assistant_settings_places),
        onBack = onBack,
        refreshing = places.loading && places.entries != null,
        onRefresh = onRefresh,
        actions = {
            JIconButton(
                PersonaIcons.add,
                stringResource(R.string.assistant_settings_places_add),
                { onOpen(null) },
                enabled = !state.busy,
                modifier = Modifier.testTag("saved-places-add"),
            )
        },
    ) {
        if (places.failed && entries.isEmpty()) item("failed") {
            JEmptyState(
                stringResource(R.string.assistant_settings_places_load_failed_title),
                places.error ?: stringResource(R.string.assistant_settings_places_load_failed_text),
                icon = PersonaIcons.place,
            )
        } else if (places.failed) item("failed-notice") {
            JNotice(
                places.error ?: stringResource(R.string.assistant_settings_places_load_failed_text),
                Modifier.padding(top = Tokens.Space.m),
                status = Status.NeedsYou,
                actionLabel = stringResource(R.string.assistant_settings_retry),
                onAction = onRefresh,
            )
        }
        if (places.entries == null && places.loading) item("loading") {
            JLoading(text = stringResource(R.string.assistant_settings_loading))
        }
        if (places.entries != null && entries.isEmpty() && !places.failed) item("empty") {
            JEmptyState(
                stringResource(R.string.assistant_settings_places_empty_title),
                stringResource(R.string.assistant_settings_places_empty_text),
                icon = PersonaIcons.place,
            )
        }
        itemsIndexed(entries, key = { _, place -> place.id }) { index, place ->
            JGroupItem(GroupPosition.of(index, entries.size)) {
                JRow(
                    place.name,
                    subtitle = SavedPlaceText.detail(place),
                    leadingIcon = PersonaIcons.place,
                    value = SavedPlaceText.radius(place.radiusMeters, locale),
                    titleLines = 1,
                    chevron = true,
                    onClick = { onOpen(place.id) },
                    onLongClick = { pendingDelete = place },
                    modifier = Modifier.testTag("saved-place-${place.id}"),
                )
            }
        }
        // Section footer below the card (same place as iOS List section footer; #9293 S1).
        if (entries.isNotEmpty()) item("footer") {
            Caption(stringResource(R.string.assistant_settings_places_footer))
        }
    }
    pendingDelete?.let { place ->
        JDialog(
            onDismiss = { pendingDelete = null },
            title = stringResource(R.string.assistant_settings_places_delete_title, place.name),
            text = stringResource(R.string.assistant_settings_places_delete_text),
            destructive = true,
            confirmEnabled = !state.busy,
            confirm = stringResource(R.string.assistant_settings_delete) to {
                pendingDelete = null
                scope.launch { onDelete(place) }
                Unit
            },
            dismiss = stringResource(R.string.assistant_settings_cancel) to { pendingDelete = null },
        )
    }
}

/** New or existing place editor: name, location (search / current), reminder radius; no map (#9293). */
@Composable
internal fun SavedPlaceEditorPart(
    state: AssistantSettingsUiState,
    placeId: String?,
    onBack: () -> Unit,
    onSave: suspend (SavedPlace) -> SavedPlace?,
    onDelete: suspend (SavedPlace) -> Boolean,
) {
    val resources = LocalResources.current
    val context = LocalContext.current
    val locale = currentLocale
    val scope = rememberCoroutineScope()
    val existing = placeId?.let { id -> state.places.entries?.firstOrNull { it.id == id } }
    val creating = placeId == null
    val awaitingPlace = placeId != null && existing == null && state.places.entries == null
    val missingPlace = placeId != null && existing == null && state.places.entries != null
    var name by rememberSaveable(placeId) { mutableStateOf(existing?.name.orEmpty()) }
    var address by rememberSaveable(placeId) { mutableStateOf(existing?.address.orEmpty()) }
    var latitude by rememberSaveable(placeId) { mutableStateOf(existing?.latitude ?: Double.NaN) }
    var longitude by rememberSaveable(placeId) { mutableStateOf(existing?.longitude ?: Double.NaN) }
    var radius by rememberSaveable(placeId) { mutableIntStateOf(existing?.radiusMeters ?: SavedPlace.DEFAULT_RADIUS) }
    var search by rememberSaveable(placeId) { mutableStateOf("") }
    var hits by remember { mutableStateOf<List<SavedPlaceSearchHit>>(emptyList()) }
    var searchNote by remember { mutableStateOf<String?>(null) }
    var locating by remember { mutableStateOf(false) }
    var searching by remember { mutableStateOf(false) }
    var deleting by remember { mutableStateOf(false) }
    var locationHint by remember { mutableStateOf<String?>(null) }
    var searchJob by remember { mutableStateOf<Job?>(null) }
    val opened = remember(placeId, existing) {
        existing ?: SavedPlace(radiusMeters = SavedPlace.DEFAULT_RADIUS)
    }
    val located = !latitude.isNaN() && !longitude.isNaN()
    // Use placeId (not existing?.id) so a restore after process death still PUTs (#9293 R2).
    val draft = SavedPlace(
        id = placeId.orEmpty(),
        name = name,
        address = address,
        latitude = if (located) latitude else 0.0,
        longitude = if (located) longitude else 0.0,
        radiusMeters = radius,
    )
    val dirty = creating || !SavedPlaceText.sameEdit(draft, opened)
    val canSave = SavedPlaceText.editorReady(placeId, state.places.entries) &&
        SavedPlaceText.canSave(draft, located) && dirty && !state.busy
    val permissionLauncher = rememberLauncherForActivityResult(
        ActivityResultContracts.RequestMultiplePermissions(),
    ) { granted ->
        if (granted.values.any { it }) {
            scope.launch { useCurrentLocation(context, { locating = it }, { locationHint = it }) { lat, lon, addr ->
                latitude = lat; longitude = lon; address = addr
            } }
        } else {
            locationHint = resources.getString(R.string.assistant_settings_places_location_denied)
        }
    }

    // After process death the id is restored but fields may still be empty until the list loads.
    LaunchedEffect(existing) {
        val place = existing ?: return@LaunchedEffect
        if (name.isEmpty() && address.isEmpty() && latitude.isNaN()) {
            name = place.name
            address = place.address
            latitude = place.latitude
            longitude = place.longitude
            radius = place.radiusMeters
        }
    }

    LaunchedEffect(search) {
        searchJob?.cancel()
        val query = search.trim()
        if (query.length < 2) {
            hits = emptyList()
            searchNote = null
            return@LaunchedEffect
        }
        searchJob = scope.launch {
            delay(350)
            searching = true
            try {
                val found = SavedPlaceLocation.forwardGeocode(context, query)
                when {
                    found == null -> {
                        hits = emptyList()
                        searchNote = resources.getString(R.string.assistant_settings_places_geocoder_missing)
                    }
                    found.isEmpty() -> {
                        hits = emptyList()
                        searchNote = resources.getString(R.string.assistant_settings_places_search_nothing, query)
                    }
                    else -> {
                        hits = found
                        searchNote = null
                    }
                }
            } catch (error: CancellationException) { throw error }
            catch (_: Exception) {
                hits = emptyList()
                searchNote = resources.getString(R.string.assistant_settings_places_search_failed)
            } finally {
                searching = false
            }
        }
    }

    fun pick(hit: SavedPlaceSearchHit) {
        latitude = hit.latitude
        longitude = hit.longitude
        address = hit.address.ifBlank { hit.name }
        if (name.trim().isEmpty()) name = hit.name.take(40)
        search = ""
        hits = emptyList()
        searchNote = null
        locationHint = null
    }

    fun requestCurrent() {
        locationHint = null
        if (!SavedPlaceLocation.hasLocationPermission(context)) {
            permissionLauncher.launch(SavedPlaceLocation.locationPermissions)
            return
        }
        scope.launch {
            useCurrentLocation(context, { locating = it }, { locationHint = it }) { lat, lon, addr ->
                latitude = lat; longitude = lon; address = addr
            }
        }
    }

    JListPage(
        title = if (creating) stringResource(R.string.assistant_settings_places_new)
        else existing?.name ?: stringResource(R.string.assistant_settings_places),
        onBack = onBack,
        bottomBar = {
            Row(
                Modifier.fillMaxWidth().padding(horizontal = pagePadding, vertical = Tokens.Space.s),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                JButton(
                    if (creating) stringResource(R.string.assistant_settings_places_add)
                    else stringResource(R.string.assistant_settings_save),
                    {
                        scope.launch {
                            if (onSave(draft) != null) onBack()
                        }
                    },
                    Modifier.fillMaxWidth().testTag("saved-place-save"),
                    enabled = canSave,
                    loading = state.busy,
                )
            }
        },
    ) {
        if (awaitingPlace) item("awaiting") {
            JLoading(text = stringResource(R.string.assistant_settings_loading))
        }
        if (missingPlace) item("missing") {
            JNotice(
                stringResource(R.string.assistant_settings_places_load_failed_text),
                Modifier.padding(top = Tokens.Space.m),
                status = Status.NeedsYou,
            )
        }
        state.notice?.takeIf { it.error }?.let { notice ->
            item("notice") {
                JNotice(notice.text.resolve(resources), Modifier.padding(top = Tokens.Space.m), status = Status.NeedsYou)
            }
        }
        item("name-header") { JSectionHeader(stringResource(R.string.assistant_settings_places_name)) }
        item("name") {
            JGroupItem(GroupPosition.Single) {
                JTextField(
                    name,
                    { name = it },
                    Modifier.fillMaxWidth().testTag("saved-place-name"),
                    placeholder = stringResource(R.string.assistant_settings_places_name_placeholder),
                    enabled = !state.busy && !awaitingPlace && !missingPlace,
                    singleLine = true,
                )
            }
        }
        item("name-caption") { Caption(stringResource(R.string.assistant_settings_places_name_caption)) }

        item("location-header") { JSectionHeader(stringResource(R.string.assistant_settings_places_location)) }
        if (located) item("chosen") {
            JGroupItem(GroupPosition.Single) {
                Column(Modifier.padding(horizontal = Tokens.Space.m, vertical = Tokens.Space.s)) {
                    Text(
                        address.ifBlank { SavedPlaceText.coordinates(draft) },
                        style = PersonaTheme.type.body,
                        color = PersonaTheme.palette.ink,
                        maxLines = 3,
                        overflow = TextOverflow.Ellipsis,
                    )
                    if (address.isNotBlank()) {
                        Text(
                            SavedPlaceText.coordinates(draft),
                            style = PersonaTheme.type.caption,
                            color = PersonaTheme.palette.ink3,
                            modifier = Modifier.padding(top = Tokens.Space.xs),
                        )
                    }
                }
            }
        }
        item("search") {
            JGroupItem(GroupPosition(first = true, last = false)) {
                JTextField(
                    search,
                    { search = it },
                    Modifier.fillMaxWidth().testTag("saved-place-search"),
                    // Placeholder only — a label duplicates the same text and clips in Glass (#9293 S2).
                    placeholder = stringResource(R.string.assistant_settings_places_search),
                    enabled = !state.busy && !locating && !awaitingPlace && !missingPlace,
                    singleLine = true,
                    trailing = if (search.isNotEmpty()) ({
                        JIconButton(PersonaIcons.close, stringResource(R.string.assistant_settings_places_clear_search), { search = "" })
                    }) else null,
                )
            }
        }
        item("current") {
            JGroupItem(GroupPosition(first = false, last = true)) {
                JRow(
                    stringResource(R.string.assistant_settings_places_use_current),
                    leadingIcon = PersonaIcons.place,
                    chevron = false,
                    enabled = !state.busy && !locating && !awaitingPlace && !missingPlace,
                    onClick = { requestCurrent() },
                    modifier = Modifier.testTag("saved-place-current"),
                )
            }
        }
        if (searching) item("searching") {
            JLoading(Modifier.padding(top = Tokens.Space.s), text = stringResource(R.string.assistant_settings_loading))
        }
        hits.forEachIndexed { index, hit ->
            item("hit-$index") {
                JGroupItem(GroupPosition.of(index, hits.size)) {
                    JRow(
                        hit.name,
                        subtitle = hit.address.takeIf { it.isNotBlank() && it != hit.name },
                        leadingIcon = PersonaIcons.place,
                        chevron = false,
                        onClick = { pick(hit) },
                    )
                }
            }
        }
        (searchNote ?: locationHint)?.let { note ->
            item("location-note") {
                Caption(note)
            }
        }
        if (searchNote == null && locationHint == null) item("location-caption") {
            Caption(stringResource(R.string.assistant_settings_places_location_caption))
        }

        item("radius-header") { JSectionHeader(stringResource(R.string.assistant_settings_places_radius)) }
        val choices = SavedPlaceText.radiiWith(radius)
        choices.forEachIndexed { index, meters ->
            item("radius-$meters") {
                JGroupItem(GroupPosition.of(index, choices.size)) {
                    JRow(
                        SavedPlaceText.radius(meters, locale),
                        leading = { JRadio(selected = meters == radius, onClick = null) },
                        chevron = false,
                        enabled = !state.busy && !awaitingPlace && !missingPlace,
                        onClick = { radius = meters },
                    )
                }
            }
        }
        item("radius-caption") { Caption(stringResource(R.string.assistant_settings_places_radius_caption)) }

        // Delete only as a button at the end (iOS has no top-bar trash; #9293 S3).
        if (!creating && existing != null) item("delete") {
            Row(
                Modifier.fillMaxWidth().padding(top = Tokens.Space.l),
                horizontalArrangement = Arrangement.Center,
            ) {
                JButton(
                    stringResource(R.string.assistant_settings_places_delete),
                    { deleting = true },
                    kind = JButtonKind.Destructive,
                    enabled = !state.busy,
                )
            }
        }
    }

    if (deleting && existing != null) JDialog(
        onDismiss = { deleting = false },
        title = stringResource(R.string.assistant_settings_places_delete_title, existing.name),
        text = stringResource(R.string.assistant_settings_places_delete_text),
        destructive = true,
        confirmEnabled = !state.busy,
        confirm = stringResource(R.string.assistant_settings_delete) to {
            deleting = false
            scope.launch { if (onDelete(existing)) onBack() }
            Unit
        },
        dismiss = stringResource(R.string.assistant_settings_cancel) to { deleting = false },
    )
}

private suspend fun useCurrentLocation(
    context: android.content.Context,
    setLocating: (Boolean) -> Unit,
    setHint: (String?) -> Unit,
    onFix: (Double, Double, String) -> Unit,
) {
    setLocating(true)
    setHint(null)
    try {
        if (!Geocoder.isPresent()) {
            // Still allow a fix without reverse geocoding.
        }
        val fix = SavedPlaceLocation.currentFix(context)
        if (fix == null) {
            setHint(context.getString(
                if (!SavedPlaceLocation.hasLocationPermission(context))
                    R.string.assistant_settings_places_location_denied
                else R.string.assistant_settings_places_location_failed,
            ))
            return
        }
        val addr = try {
            SavedPlaceLocation.reverseGeocode(context, fix.latitude, fix.longitude)
        } catch (error: CancellationException) { throw error }
        catch (_: Exception) { "" }
        onFix(fix.latitude, fix.longitude, addr)
    } finally {
        setLocating(false)
    }
}
