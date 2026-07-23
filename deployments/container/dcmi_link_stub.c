/*
 * Copyright 2026 The HAMi Authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

/*
 * hami-vnpu-core links against DCMI, while the CANN devel image does not
 * contain the driver-owned libdcmi.so. This build-only stub records the
 * libdcmi.so DT_NEEDED dependency without copying a host driver library into
 * the image. The real symbols are provided by the node driver at runtime.
 */
int dcmi_init(void) {
	return -1;
}

int dcmi_get_card_list(int *card_num, int *card_list, int list_len) {
	(void)card_num;
	(void)card_list;
	(void)list_len;
	return -1;
}

int dcmi_get_device_resource_info(int card_id, int device_id, void *proc_info, int *proc_num) {
	(void)card_id;
	(void)device_id;
	(void)proc_info;
	(void)proc_num;
	return -1;
}
