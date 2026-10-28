// Inert Root boundary: pure binding, never launches an owner or provider.
import {prepareHikyo158PartialKMSDeletion} from './dependencies/native_partial_kms_deletion_prepare.source-only.mjs';
import {createRequire} from 'node:module';
const require=createRequire(import.meta.url),{cleanupBinding}=require('./binding.cjs'),pins=require('./native-pins.json');
export async function prepareFromTicket(binding,explicitNowMS){
 const preparation=prepareHikyo158PartialKMSDeletion(binding.ticket.nativeHelperInput,explicitNowMS);
 await cleanupBinding(globalThis,binding,preparation,pins);return preparation;
}
